import { plural } from "./plural.js";

// What the page says about a Proxyshop import: its progress, and how it ended.
// It reads nothing from the document, so it is tested on its own

// STOPPED is the error a stopped import ends with
export const STOPPED = "The import was stopped";

// percent writes a fraction of the work as a whole percentage
export const percent = (frac) => `${Math.round(Math.min(1, Math.max(0, Number(frac) || 0)) * 100)}%`;

// progressLine is the line under the bar while an import runs. A stop that was
// asked for is said as soon as it is, since the conversion finishes the part it
// is drawing first
export function progressLine(step, frac, stopping = false) {
  if (stopping) return "Stopping after the part being drawn…";
  return `${step || "Starting…"} ${percent(frac)}`;
}

// outcome is how an import ended, as { kind, title, text, notes, id }, where kind
// is ok, stopped or err and title is the short form a toast shows. imported is the last event's imported, and error the message the
// job failed with
export function outcome(imported, error) {
  if (error) {
    if (error === STOPPED) return { kind: "stopped", title: "Import stopped", text: "The import was stopped, and nothing was installed.", notes: [], id: "" };
    return { kind: "err", title: "Import failed", text: `The import failed: ${error}`, notes: [], id: "" };
  }
  const notes = imported.notes || [];
  const title = `Imported ${imported.name} ${imported.version}`;
  return { kind: "ok", title, text: `${title}. It is marked in the list below.`, notes, id: imported.id };
}

// notesHeading names the conversion's notes for the control that shows them
export const notesHeading = (n) => (n ? `${plural(n, "note")} on what the conversion left out or draws differently` : "The conversion left nothing out");
