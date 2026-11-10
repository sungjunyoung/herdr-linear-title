import { test } from "node:test";
import assert from "node:assert/strict";
import { AUTO, FORCE, due, decide, afterResolve, parseResolve, needsSetup } from "./policy.js";

const space = (over = {}) => ({ id: "s1", name: "backoffice", named: false, machine: null, branch: "homeco-1-x", ...over });
const ok = { status: "ok", identifier: "HOMECO-1", title: "Fix", label: "HOMECO-1 Fix" };

test("due: new, branch switch and refresh rules", () => {
  assert.equal(due(undefined, "a"), true);
  assert.equal(due({ branch: "a", state: "done" }, "a"), false);
  assert.equal(due({ branch: "a", state: "done" }, "b"), true, "branch switch retitles");
  assert.equal(due({ branch: "a", state: "failed" }, "a"), false, "failures are not retried");
  assert.equal(due({ branch: "a", state: "failed" }, "b"), true);
  assert.equal(due({ branch: "a", state: "pending" }, "b"), false, "one job per space");
});

test("decide", () => {
  assert.deepEqual(decide(space(), {}, AUTO), { kind: "resolve" });
  assert.deepEqual(decide(space({ machine: "vps" }), {}, FORCE), { kind: "skip", reason: "remote" });
  assert.deepEqual(decide(space({ branch: null }), {}, FORCE), { kind: "skip", reason: "no_branch" });
  assert.deepEqual(decide(space({ named: true, name: "mine" }), {}, AUTO), { kind: "skip", reason: "user_named" });
  assert.deepEqual(decide(space({ named: true, name: "HOMECO-1 Old" }), { s1: "HOMECO-1 Old" }, AUTO), { kind: "resolve" });
  assert.deepEqual(decide(space({ named: true, name: "mine" }), {}, FORCE), { kind: "resolve" });
});

test("afterResolve", () => {
  assert.deepEqual(afterResolve(space(), ok, {}, AUTO), { kind: "rename", label: "HOMECO-1 Fix" });
  assert.deepEqual(afterResolve(space({ named: true, name: "HOMECO-1 Fix" }), ok, {}, FORCE), { kind: "unchanged", label: "HOMECO-1 Fix" });
  assert.deepEqual(afterResolve(space({ named: true, name: "mine" }), ok, {}, AUTO), { kind: "skip", reason: "user_named" }, "renamed during resolve");
  assert.deepEqual(afterResolve(space({ named: true, name: "mine" }), ok, {}, FORCE), { kind: "rename", label: "HOMECO-1 Fix" });
  assert.deepEqual(afterResolve(space(), { status: "no_ref" }, {}, AUTO), { kind: "skip", reason: "no_ref" });
  assert.deepEqual(afterResolve(space(), { status: "error", message: "boom" }, {}, AUTO), { kind: "error", message: "boom" });
});

test("parseResolve reads stdout whatever the exit code", () => {
  assert.deepEqual(parseResolve({ code: 1, stdout: '{"status":"error","message":"m"}\n', stderr: "x" }), { status: "error", message: "m" });
  assert.deepEqual(parseResolve({ code: 0, stdout: '{"status":"no_ref"}', stderr: "" }), { status: "no_ref" });
  assert.deepEqual(parseResolve({ code: 2, stdout: "", stderr: "a\nusage: x\n" }), { status: "error", message: "usage: x" });
  assert.deepEqual(parseResolve({ code: 2, stdout: "", stderr: "" }), { status: "error", message: "resolve exited with code 2." });
});

test("needsSetup matches the Go setup hint", () => {
  assert.equal(needsSetup('Not configured. Run setup ("Linear: setup / login" in herdr, or herdr-linear-title setup).'), true);
  assert.equal(needsSetup("HOMECO-1: issue not found"), false);
});
