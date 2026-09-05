const assert = require("node:assert/strict");
const {webcrypto} = require("node:crypto");
const {readFileSync} = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

async function startBackground(directory, pairings = [], identifyData = null) {
  const listeners = {};
  const requests = [];
  const errors = [];
  let automaticIdentify;
  const stored = {
    octopusVerificationBridgeV2: {
      version: 2,
      selectedKey: pairings[0]?.key || null,
      pairings,
    },
  };
  const event = (name) => ({addListener(listener) { listeners[name] = listener; }});
  const chrome = {
    runtime: {
      onInstalled: event("installed"),
      onStartup: event("startup"),
      onMessage: event("message"),
      sendMessage: async () => {},
    },
    alarms: {onAlarm: event("alarm"), create: async () => {}},
    storage: {local: {
      get: async () => structuredClone(stored),
      set: async (value) => Object.assign(stored, structuredClone(value)),
    }},
    tabs: {
      onUpdated: event("tabUpdated"),
      onRemoved: event("tabRemoved"),
      update: async () => {},
    },
    action: {
      setBadgeBackgroundColor: async () => {},
      setBadgeText: async () => {},
      setTitle: async () => {},
    },
  };
  const context = vm.createContext({
    chrome, URL, URLSearchParams, crypto: webcrypto, setTimeout, clearTimeout,
    console: {warn() {}, error(...args) { errors.push(args); }},
    fetch: async (url, options) => {
      const body = JSON.parse(options.body);
      requests.push({url, body});
      if (url.endsWith("/identify") && body.pairing_token === "test-fragment-token") {
        await automaticIdentify;
      }
      // 自定义 identifyData 只作用于 fragment 令牌：启动时的后台刷新链使用已存令牌，
      // 不能让 mock 响应污染已存配对的 identity 而破坏「不同 pairing id」等用例的前提。
      const data = body.pairing_token === "test-fragment-token" && identifyData
        ? identifyData
        : {pairing: {id: 1}, latest_task: null};
      return {
        ok: true,
        status: 200,
        json: async () => ({code: 200, data: structuredClone(data)}),
      };
    },
    importScripts: (filename) => {
      vm.runInContext(readFileSync(path.join(directory, filename), "utf8"), context);
    },
  });
  vm.runInContext(readFileSync(path.join(directory, "background.js"), "utf8"), context);
  const settle = async () => {
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(errors, []);
  };
  await settle();
  requests.length = 0;
  return {
    requests,
    deferAutomaticIdentify() {
      let release;
      automaticIdentify = new Promise((resolve) => { release = resolve; });
      return async () => {
        release();
        await settle();
      };
    },
    async openFragment(origin) {
      const fragment = new URLSearchParams({
        octopus_sync: "1", octopus_token: "test-fragment-token", octopus_origin: origin,
      });
      listeners.tabUpdated(1, {}, {url: `https://untrusted.example/#${fragment}`});
      await settle();
    },
    async message(message) {
      const response = await new Promise((resolve) => listeners.message(message, {}, resolve));
      await settle();
      return response;
    },
    pairings: () => stored.octopusVerificationBridgeV2.pairings,
  };
}

const trustedOrigin = "https://trusted-octopus.example";
const savedPairing = () => ({
  key: "saved-pairing", baseURL: trustedOrigin, pairingToken: "test-saved-token",
  identity: {pairing: {id: 1}},
});

for (const directory of [__dirname, path.resolve(__dirname, "../../static/extensions/verification-bridge")]) {
  const label = path.relative(path.resolve(__dirname, "../.."), directory);

  test(`${label}: a webpage cannot establish first trust`, async () => {
    const background = await startBackground(directory);
    await background.openFragment("https://fake-octopus.example");
    assert.equal(background.requests.length, 0);
    assert.equal(background.pairings().length, 0);
  });

  test(`${label}: manual popup pairing establishes trust`, async () => {
    const background = await startBackground(directory);
    const response = await background.message({
      type: "pairing.add", baseURL: trustedOrigin, pairingToken: "test-manual-token",
    });
    assert.equal(response.ok, true);
    assert.equal(background.pairings().length, 1);
    assert.equal(background.pairings()[0].baseURL, trustedOrigin);
    assert.ok(background.requests.every((request) => request.url.startsWith(`${trustedOrigin}/`)));
    background.requests.length = 0;
    await background.openFragment(trustedOrigin);
    assert.ok(background.requests.some((request) => request.body.pairing_token === "test-fragment-token"));
  });

  test(`${label}: existing pairings permit only the exact trusted origin`, async () => {
    for (const origin of ["https://fake-octopus.example", "http://trusted-octopus.example", "https://trusted-octopus.example:8443"]) {
      const background = await startBackground(directory, [savedPairing()]);
      await background.openFragment(origin);
      assert.equal(background.requests.length, 0);
      assert.equal(background.pairings().length, 1);
    }
    const background = await startBackground(directory, [savedPairing()]);
    await background.openFragment(trustedOrigin);
    assert.ok(background.requests.some((request) => request.body.pairing_token === "test-fragment-token"));
  });

  test(`${label}: removing the last pairing revokes automatic trust`, async () => {
    const background = await startBackground(directory, [savedPairing()]);
    const response = await background.message({type: "pairing.remove", key: "saved-pairing"});
    assert.equal(response.ok, true);
    background.requests.length = 0;
    await background.openFragment(trustedOrigin);
    assert.equal(background.requests.length, 0);
    assert.equal(background.pairings().length, 0);
  });

  test(`${label}: a path-carrying variant of a trusted address is rejected before any request`, async () => {
    const background = await startBackground(directory, [savedPairing()]);
    await background.openFragment(`${trustedOrigin}/attacker-path`);
    assert.equal(background.requests.length, 0);
    assert.equal(background.pairings().length, 1);
    const state = await background.message({type: "state.get"});
    assert.equal(state.data.lastAutoPairEvent.ok, false);
    assert.match(state.data.lastAutoPairEvent.reason, /未知/);
  });

  test(`${label}: an automatic pairing for a different pairing id is rejected, not created`, async () => {
    const background = await startBackground(directory, [savedPairing()], {pairing: {id: 2}, latest_task: null});
    await background.openFragment(trustedOrigin);
    assert.ok(background.requests.some((request) =>
      request.url.endsWith("/identify") && request.body.pairing_token === "test-fragment-token"
    ));
    assert.equal(background.pairings().length, 1);
    assert.equal(background.pairings()[0].pairingToken, "test-saved-token");
    assert.equal(background.pairings()[0].identity.pairing.id, 1);
    const state = await background.message({type: "state.get"});
    assert.equal(state.data.lastAutoPairEvent.ok, false);
    assert.match(state.data.lastAutoPairEvent.reason, /手动确认/);
  });

  test(`${label}: a rejected automatic pairing is visible in the popup state`, async () => {
    const background = await startBackground(directory);
    await background.openFragment("https://fake-octopus.example");
    const state = await background.message({type: "state.get"});
    assert.equal(state.data.lastAutoPairEvent.ok, false);
    assert.equal(state.data.lastAutoPairEvent.baseURL, "https://fake-octopus.example");
  });

  for (const pairAgain of [false, true]) {
    test(`${label}: revocation cancels in-flight automatic pairing (pair again: ${pairAgain})`, async () => {
      const background = await startBackground(directory, [savedPairing()]);
      const releaseIdentify = background.deferAutomaticIdentify();
      await background.openFragment(trustedOrigin);
      assert.ok(background.requests.some((request) =>
        request.url.endsWith("/identify") && request.body.pairing_token === "test-fragment-token"
      ));
      const response = await background.message({type: "pairing.remove", key: "saved-pairing"});
      assert.equal(response.ok, true);
      assert.equal(background.pairings().length, 0);
      if (pairAgain) {
        const manual = await background.message({
          type: "pairing.add", baseURL: trustedOrigin, pairingToken: "test-manual-token",
        });
        assert.equal(manual.ok, true);
      }
      await releaseIdentify();
      assert.equal(background.pairings().length, pairAgain ? 1 : 0);
      if (pairAgain) {
        assert.equal(background.pairings()[0].pairingToken, "test-manual-token");
      }
    });
  }
}
