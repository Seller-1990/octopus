import assert from "node:assert/strict";
import test from "node:test";
import { shouldLogoutOnUnauthorized } from "./session-guard.ts";

// F05 回归矩阵：401 只能登出「发出该请求的那个会话」。

test("old request's 401 arriving after re-login must not log out the new session", () => {
    assert.equal(shouldLogoutOnUnauthorized("old-session", "new-session"), false);
});

test("a real 401 for the current session logs out", () => {
    assert.equal(shouldLogoutOnUnauthorized("same-session", "same-session"), true);
});

test("401 for a request sent without a token must not log out", () => {
    assert.equal(shouldLogoutOnUnauthorized(null, "some-session"), false);
    assert.equal(shouldLogoutOnUnauthorized(undefined, "some-session"), false);
    assert.equal(shouldLogoutOnUnauthorized("", "some-session"), false);
});

test("401 arriving after logout must not re-trigger a logout", () => {
    assert.equal(shouldLogoutOnUnauthorized("expired-session", null), false);
});
