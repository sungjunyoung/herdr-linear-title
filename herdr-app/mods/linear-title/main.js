// Renames spaces on a Linear branch to "<ISSUE-ID> <title>", like the herdr
// TUI plugin, by asking `herdr-linear-title resolve <branch>`.
// Commands: linear-title.refresh, .refresh-all, .setup, .logout.

import { View, div } from "gpui-kit";
import { h_flex } from "gpui-base";
import { exec, focused, home, machines, nextCommand, notify, run, setShared, shared, spaces, theme } from "herdr";
import { AUTO, FORCE, afterResolve, decide, due, footerText, parseResolve, withOwned } from "./policy.js";

const OWNED = "linear-title.owned";
const TITLE = "Linear Title";
// The footer block hugs its text: the layout node with this id is resized to
// the measured text plus padding, up to MAX_WIDTH, past which the text truncates.
const BLOCK = "linear-title";
const PAD = 14;
const MAX_WIDTH = 420;
const SKIPPED = {
  no_branch: "This space has no git branch.",
};

export default class LinearTitle extends View {
  init(_props, cx) {
    this.bin = `${home()}/.local/bin/herdr-linear-title`;
    this.jobs = new Map();
    this.notified = new Set();
    this.busy = false;
    this.status = { text: "", error: false, setup: false };
    // spaces() changes re-render, but render must stay free of side effects;
    // a cheap in-memory scan each second finds new spaces and branch switches.
    cx.timer.every(1000, (cx) => this.reconcile(cx));
    this.reconcile(cx);
    cx.spawn(async (cx) => {
      for (;;) {
        await this.command(await nextCommand());
        cx.notify();
      }
    });
  }

  owned() {
    return shared(OWNED) ?? {};
  }

  // A machine whose layout is not ready cannot take a rename; its spaces wait
  // without a job, so they are picked up once it connects.
  reachable(space) {
    return space.machine === null || machines().some((m) => m.name === space.machine && m.layout === "ready");
  }

  dueSpaces(mode) {
    return spaces().filter(
      (s) => this.reachable(s) && due(this.jobs.get(s.id), s.branch) && decide(s, this.owned(), mode).kind === "resolve",
    );
  }

  reconcile(cx) {
    if (this.busy || this.dueSpaces(AUTO).length === 0) return;
    this.busy = true;
    cx.spawn(async (cx) => {
      try {
        // Sequential on purpose: one Linear request at a time, and each space
        // is re-checked right before its turn.
        for (const s of this.dueSpaces(AUTO)) {
          const fresh = spaces().find((x) => x.id === s.id);
          if (!fresh || !due(this.jobs.get(s.id), fresh.branch) || decide(fresh, this.owned(), AUTO).kind !== "resolve") continue;
          this.show(await this.retitle(fresh, AUTO), false);
          cx.notify();
        }
      } finally {
        this.busy = false;
      }
    });
  }

  async retitle(space, mode) {
    const job = { branch: space.branch, state: "pending" };
    this.jobs.set(space.id, job);
    let res;
    try {
      res = parseResolve(await exec(this.bin, ["resolve", job.branch]));
    } catch (e) {
      res = { status: "error", message: `Cannot run ${this.bin} (${e}). Run make install-app.` };
    }
    const now = spaces().find((s) => s.id === space.id);
    // A branch switch during the lookup leaves the job stale; the next scan redoes it.
    let out = !now || now.branch !== job.branch ? { kind: "skip", reason: "moved" } : afterResolve(now, res, this.owned(), mode);
    if (out.kind === "rename") {
      try {
        run("space.rename", { id: space.id, name: out.label });
      } catch (e) {
        // Structural commands fail while the space's machine is unavailable.
        out = { kind: "error", message: `Cannot rename the space (${e}).`, setup: false };
      }
    }
    if (out.kind === "rename" || out.kind === "unchanged") this.own(space.id, out.label);
    job.state = out.kind === "error" ? "failed" : "done";
    return out;
  }

  own(id, label) {
    const owned = this.owned();
    if (owned[id] === label) return;
    setShared(OWNED, withOwned(owned, new Set(spaces().map((s) => s.id)), id, label));
  }

  // Auto runs report errors once per message; commands always answer.
  show(out, explicit) {
    if (out.kind === "error") {
      this.status = { text: `Linear: ${out.message}`, error: true, setup: out.setup };
      if (explicit || !this.notified.has(out.message)) {
        this.notified.add(out.message);
        notify({
          title: TITLE,
          body: out.message,
          level: "error",
          ...(this.status.setup ? { action: { name: "linear-title.setup" } } : {}),
        });
      }
      return;
    }
    // A success clears the last error; the footer then shows the active space's issue.
    if (out.kind === "rename" || out.kind === "unchanged") this.status = { text: "", error: false, setup: false };
    if (!explicit) return;
    const body = {
      rename: `Renamed to ${out.label}`,
      unchanged: `Already ${out.label}`,
      skip: SKIPPED[out.reason] ?? "This branch has no Linear issue reference.",
    }[out.kind];
    notify({ title: TITLE, body, level: out.kind === "skip" ? "info" : "success" });
  }

  async command(name) {
    if (name === "refresh") return this.refresh();
    if (name === "refresh-all") return this.refreshAll();
    if (name === "setup") return this.setup();
    if (name === "logout") return this.logout();
  }

  async refresh() {
    const id = focused()?.space;
    const s = spaces().find((x) => x.id === id) ?? spaces().find((x) => x.active);
    if (!s || this.jobs.get(s.id)?.state === "pending") return;
    const d = decide(s, this.owned(), FORCE);
    this.show(d.kind === "skip" ? d : await this.retitle(s, FORCE), true);
  }

  async refreshAll() {
    const counts = { rename: 0, unchanged: 0, skip: 0 };
    const failures = new Set();
    for (const s of spaces()) {
      if (this.jobs.get(s.id)?.state === "pending" || decide(s, this.owned(), FORCE).kind !== "resolve") continue;
      const out = await this.retitle(s, FORCE);
      if (out.kind === "error") failures.add(out.message);
      else counts[out.kind]++;
      if (out.kind !== "skip") this.show(out, false);
    }
    const summary = `${counts.rename} renamed, ${counts.unchanged} unchanged, ${counts.skip} without issue`;
    if (failures.size) notify({ title: TITLE, body: `${summary}. Failed: ${[...failures].join("; ")}`, level: "error" });
    else notify({ title: TITLE, body: summary, level: "success" });
  }

  setup() {
    run("tab.new", { name: "Linear setup", argv: [this.bin, "setup"] });
  }

  async logout() {
    let r;
    try {
      r = await exec(this.bin, ["logout"]);
    } catch (e) {
      return notify({ title: TITLE, body: `Cannot run ${this.bin} (${e}).`, level: "error" });
    }
    // The first stderr line is the user-facing message; a failure adds the raw error after it.
    const body = (r.stderr || r.stdout).trim().split("\n")[0] || `logout exited with code ${r.code}.`;
    notify({ title: TITLE, body, level: r.code === 0 ? "success" : "error" });
  }

  fit(width) {
    const px = Math.ceil(width) + PAD * 2 + 1;
    if (px === this.width) return;
    this.width = px;
    try {
      run("layout.resize", { id: BLOCK, px });
    } catch {
      // No such node: the mod runs as a kept window, which has its own size.
    }
  }

  render() {
    const t = theme();
    const s = this.status;
    const text = footerText(s, spaces().find((x) => x.active), this.owned());
    return h_flex()
      .id("linear-title")
      .size_full()
      .px(PAD)
      .items_center()
      .justify_end()
      .bg(t.chrome)
      .border_t(1)
      .border_color(t.border)
      .text_size(11.8)
      .text_color(s.error ? t.blocked : t.dim)
      .when(s.setup, (el) => el.cursor_pointer().hover((st) => st.text_color(t.foreground)).on_click(() => this.setup()))
      .child(
        div()
          .id("linear-title-text")
          .flex_none()
          .max_w(MAX_WIDTH - PAD * 2)
          .truncate()
          .on_size_change((size) => this.fit(size.width))
          .child(s.setup ? `${text} Click to set up.` : text),
      );
  }
}
