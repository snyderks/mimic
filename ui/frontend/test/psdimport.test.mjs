import test from "node:test";
import assert from "node:assert/strict";

const { STOPPED, percent, progressLine, outcome, notesHeading } = await import("../js/psdimport.js");

test("percent rounds and holds the fraction to the bar", () => {
  assert.equal(percent(0.424), "42%");
  assert.equal(percent(1.2), "100%");
  assert.equal(percent(-1), "0%");
  assert.equal(percent(undefined), "0%");
});

test("the progress line names the step, or that a stop was asked for", () => {
  assert.equal(progressLine("Drawing normal_background, part 1 of 4…", 0.05), "Drawing normal_background, part 1 of 4… 5%");
  assert.equal(progressLine("", 0), "Starting… 0%");
  assert.equal(progressLine("Drawing x", 0.5, true), "Stopping after the part being drawn…");
});

test("an import that installed a template says so and keeps its notes", () => {
  const got = outcome({ id: "proxyshop.normal", name: "Normal (Proxyshop)", version: "0.1.0", notes: ["a", "b"] });
  assert.deepEqual(got, {
    kind: "ok",
    title: "Imported Normal (Proxyshop) 0.1.0",
    text: "Imported Normal (Proxyshop) 0.1.0. It is marked in the list below.",
    notes: ["a", "b"],
    id: "proxyshop.normal",
  });
  assert.deepEqual(outcome({ id: "p.x", name: "X", version: "1.0.0", notes: null }).notes, []);
});

test("a stopped import is not a failure", () => {
  assert.equal(outcome(null, STOPPED).kind, "stopped");
  assert.deepEqual(outcome(null, "not a Photoshop document"), { kind: "err", title: "Import failed", text: "The import failed: not a Photoshop document", notes: [], id: "" });
});

test("the notes heading counts them", () => {
  assert.equal(notesHeading(0), "The conversion left nothing out");
  assert.equal(notesHeading(1), "1 note on what the conversion left out or draws differently");
  assert.equal(notesHeading(3), "3 notes on what the conversion left out or draws differently");
});
