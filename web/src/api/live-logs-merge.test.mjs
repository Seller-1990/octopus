import assert from "node:assert/strict";
import test from "node:test";
import { LIVE_LOGS_MAX_ENTRIES, applyLiveLogEvent } from "./live-logs-merge.ts";

const entry = (id, state) => ({ id, state });

test("merge inserts new events sorted by id descending", () => {
    let logs = [];
    logs = applyLiveLogEvent(logs, entry(3, "running"));
    logs = applyLiveLogEvent(logs, entry(1, "running"));
    logs = applyLiveLogEvent(logs, entry(2, "running"));
    assert.deepEqual(
        logs.map((item) => item.id),
        [3, 2, 1],
    );
});

test("merge replaces the same id so a running entry transitions to finished", () => {
    let logs = [entry(1, "running"), entry(2, "running")];
    logs = applyLiveLogEvent(logs, entry(1, "success"));
    assert.equal(logs.length, 2);
    assert.equal(logs.find((item) => item.id === 1).state, "success");
});

test("merge keeps the list bounded", () => {
    let logs = [];
    for (let id = 1; id <= LIVE_LOGS_MAX_ENTRIES + 50; id++) {
        logs = applyLiveLogEvent(logs, entry(id, "success"));
    }
    assert.equal(logs.length, LIVE_LOGS_MAX_ENTRIES);
    assert.equal(logs[0].id, LIVE_LOGS_MAX_ENTRIES + 50);
});

// F07：重连时 hook 先 setLogs([]) 再接收快照事件。已不存在于服务端快照的
// 旧 running 条目（离线期间完成且被完成窗口逐出）必须被清除。
test("reset then snapshot re-applies clears ghost running entries", () => {
    let logs = [entry(1, "running"), entry(2, "success")];
    // 服务端快照只含 id=2：重置后重新合并快照。
    logs = applyLiveLogEvent([], entry(2, "success"));
    assert.deepEqual(
        logs.map((item) => item.id),
        [2],
    );
    assert.equal(logs.some((item) => item.id === 1 && item.state === "running"), false);
});

test("a still-running request survives reconnect via the fresh snapshot", () => {
    let logs = [entry(1, "running")];
    // 重连：服务端快照再次包含仍在运行的 id=1。
    logs = applyLiveLogEvent([], entry(1, "running"));
    assert.equal(logs[0].state, "running");
});

test("repeated snapshot events are idempotent", () => {
    let logs = [];
    logs = applyLiveLogEvent(logs, entry(5, "running"));
    logs = applyLiveLogEvent(logs, entry(5, "running"));
    assert.equal(logs.length, 1);
    assert.equal(logs[0].id, 5);
});
