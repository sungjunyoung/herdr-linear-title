// Pure decisions for the linear-title mod. main.js owns every side effect.

export const AUTO = "auto";
export const FORCE = "force";

// A space needs work when it was never processed or its branch moved since.
// A pending job is never doubled, and a failed one waits for a branch change
// or a refresh command.
export function due(job, branch) {
  return !job || (job.state !== "pending" && job.branch !== branch);
}

// owned maps space id to the label this mod last wrote. Auto mode keeps any
// other name, because someone chose it. Spaces on other machines count too:
// the lookup needs only the branch name, so it runs on this computer.
export function decide(space, owned, mode) {
  if (!space.branch) return { kind: "skip", reason: "no_branch" };
  if (mode === FORCE || !space.named || space.name === owned[space.id]) return { kind: "resolve" };
  return { kind: "skip", reason: "user_named" };
}

// space is re-read after the resolve, so a rename the user made meanwhile wins.
export function afterResolve(space, res, owned, mode) {
  if (res.status === "no_ref") return { kind: "skip", reason: "no_ref" };
  if (res.status !== "ok") return { kind: "error", message: res.message || "Linear lookup failed.", setup: res.needs_setup === true };
  const d = decide(space, owned, mode);
  if (d.kind === "skip") return d;
  if (space.named && space.name === res.label) return { kind: "unchanged", label: res.label };
  return { kind: "rename", label: res.label };
}

// `herdr-linear-title resolve` prints one JSON object and exits 1 on error,
// so stdout is read whatever the exit code.
export function parseResolve({ code, stdout, stderr }) {
  try {
    const res = JSON.parse(stdout);
    if (res && typeof res.status === "string") return res;
  } catch {}
  const why = (stderr || stdout || "").trim().split("\n").pop();
  return { status: "error", message: why || `resolve exited with code ${code}.` };
}

// Drops entries for spaces that no longer exist, then records label for id.
export function withOwned(owned, liveIds, id, label) {
  const next = Object.fromEntries(Object.entries(owned).filter(([k]) => liveIds.has(k)));
  next[id] = label;
  return next;
}

// The footer follows the shown space: its issue while the space still carries
// the label this mod wrote, else nothing to show. The last error wins, so a
// missing setup stays clickable.
export function footerText(status, active, owned) {
  if (status.error) return status.text;
  const label = active && owned[active.id];
  return label && active.name === label ? label : "Linear: no issue";
}
