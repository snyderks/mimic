import { test, expect } from "../../bridge-fixtures.mjs";
import { done, progress } from "../../bridge/cards.mjs";

const row = (name, title, shapes = [{ role: "single", kind: "standard" }]) => ({
  name,
  title,
  standard: shapes.some((s) => s.role === "single"),
  shapes,
  versions: [{ version: "0.1.0", label: "0.1.0", cached: true, selectable: true, active: false, action: "select", removable: true, restorable: false }],
});
const m15 = row("mimic.m15", "Modern frame (M15)");
const tf = row("proxyshop.tf", "Transform (Proxyshop)", [
  { role: "transform_front", kind: "standard" },
  { role: "transform_back", kind: "standard" },
]);
const PATHS = ["/psd/tf-back.psd", "/psd/tf-front.psd"];

async function ready(bridge, page) {
  bridge.reply("Templates.List", [m15]);
  bridge.reply("System.PickFiles", PATHS);
  bridge.reply("Templates.ImportProxyshop", "imp-1");
  bridge.reply("Templates.CancelImport", null);
  await bridge.boot();
  await page.locator("#template-trigger").click();
  await expect(page.locator("#tm-library .tp-template")).toHaveCount(1);
}

const button = (page) => page.locator("#tm-import-psd");
const status = (page) => page.locator("#tm-import-status");
const line = (page) => status(page).locator(".tm-import-text");

// start clicks the button and waits for the app to be asked to convert
async function start(bridge, page) {
  await button(page).click();
  await expect.poll(() => bridge.calledWith("Templates.ImportProxyshop").length).toBe(1);
}

test("the library has Import Proxyshop PSD, and a dialog that was cancelled starts nothing", async ({ page, bridge }) => {
  await ready(bridge, page);
  bridge.reply("System.PickFiles", []);
  await expect(button(page)).toHaveText("Import Proxyshop PSD");
  await expect(button(page)).not.toHaveClass(/gated/);
  await button(page).click();
  await expect.poll(() => bridge.calledWith("System.PickFiles").length).toBe(1);
  expect(bridge.calledWith("System.PickFiles")[0][0].filters).toEqual([{ name: "Photoshop documents", pattern: "*.psd" }]);
  expect(bridge.calledWith("Templates.ImportProxyshop")).toEqual([]);
  await expect(status(page)).toBeHidden();
});

test("an import shows each part as it is drawn, then marks the template and offers the notes", async ({ page, bridge }) => {
  await ready(bridge, page);
  await start(bridge, page);
  expect(bridge.calledWith("Templates.ImportProxyshop")).toEqual([[PATHS]]);
  await expect(button(page)).toBeDisabled();
  await expect(status(page)).toBeVisible();

  await bridge.job("imp-1", [progress(1, "Reading tf-front.psd and tf-back.psd…", 0), progress(2, "Drawing front_background, part 1 of 8…", 0.05)]);
  await expect(line(page)).toHaveText("Drawing front_background, part 1 of 8… 5%");
  await expect(status(page).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "5");
  await bridge.job("imp-1", [progress(3, "Drawing back_pinlines, part 7 of 8…", 0.83)]);
  await expect(line(page)).toHaveText("Drawing back_pinlines, part 7 of 8… 83%");

  bridge.reply("Templates.List", [m15, tf]);
  const notes = ["front: the Nyx group draws without its glow", "back: the layer Shadow is left out"];
  await bridge.job("imp-1", [done(4, { imported: { id: "proxyshop.tf", name: "Transform (Proxyshop)", version: "0.1.0", notes } })]);
  await expect(line(page)).toHaveText("Imported Transform (Proxyshop) 0.1.0. It is marked in the list below.");
  await expect(page.locator("#toasts .toast").last()).toHaveText("Imported Transform (Proxyshop) 0.1.0");
  await expect(button(page)).toBeEnabled();
  await expect(status(page).getByRole("progressbar")).toHaveCount(0);

  const fresh = page.locator('#tm-library .tp-template[data-template="proxyshop.tf"]');
  await expect(fresh).toHaveClass(/tp-new/);
  await expect(page.locator("#tm-library .tp-template.tp-new")).toHaveCount(1);

  const summary = status(page).locator("summary");
  await expect(summary).toHaveText("2 notes on what the conversion left out or draws differently");
  await expect(status(page).locator("li").first()).toBeHidden();
  await summary.click();
  await expect(status(page).locator("li")).toHaveText(notes);

  await status(page).getByRole("button", { name: "Dismiss" }).click();
  await expect(status(page)).toBeHidden();
});

test("an import can be stopped, and nothing is said to be installed", async ({ page, bridge }) => {
  await ready(bridge, page);
  await start(bridge, page);
  await bridge.job("imp-1", [progress(1, "Drawing normal_background, part 1 of 4…", 0.05)]);
  await status(page).getByRole("button", { name: "Stop" }).click();
  await expect.poll(() => bridge.calledWith("Templates.CancelImport")).toEqual([["imp-1"]]);
  await expect(line(page)).toHaveText("Stopping after the part being drawn…");
  // Progress that arrives while the part finishes leaves the line saying it is stopping
  await bridge.job("imp-1", [progress(2, "Drawing normal_pinlines, part 2 of 4…", 0.3)]);
  await expect(line(page)).toHaveText("Stopping after the part being drawn…");

  await bridge.job("imp-1", [done(3, { error: "The import was stopped" })]);
  await expect(line(page)).toHaveText("The import was stopped, and nothing was installed.");
  await expect(status(page)).not.toHaveClass(/\berr\b/);
  await expect(page.locator("#tm-library .tp-template.tp-new")).toHaveCount(0);
  await expect(button(page)).toBeEnabled();
});

test("a conversion that fails says why", async ({ page, bridge }) => {
  await ready(bridge, page);
  await start(bridge, page);
  await bridge.job("imp-1", [done(1, { error: "normal.psd: psd: not a Photoshop document" })]);
  await expect(line(page)).toHaveText("The import failed: normal.psd: psd: not a Photoshop document");
  await expect(status(page)).toHaveClass(/\berr\b/);
  await expect(button(page)).toBeEnabled();
});

test("files the app will not convert are refused before anything starts", async ({ page, bridge }) => {
  await ready(bridge, page);
  bridge.fail("Templates.ImportProxyshop", "Two PSDs make one template when their names differ only in front and back, as tf-front.psd and tf-back.psd do");
  await button(page).click();
  await expect(page.locator("#toasts .toast.err").last()).toContainText("Two PSDs make one template");
  await expect(status(page)).toBeHidden();
  await expect(button(page)).toBeEnabled();
});
