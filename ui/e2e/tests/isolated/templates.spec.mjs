import path from "node:path";
import { test, expect } from "../../fixtures.mjs";

// These tests install templates, which the app keeps for good, so they run on
// the isolated server and in the order of this file

test("a catalog with no templates lists none", async ({ templates, fake }) => {
  await templates.show();
  await expect(templates.libraryList.locator(".tp-template")).toHaveCount(0);
  await expect(templates.label).toHaveText("No template installed");
});

test("downloading a template installs it, makes it active and renders with it", async ({ templates, single, fake, page }) => {
  await fake.catalog([{ name: "test.normal", version: "1.0.0" }, { name: "test.transform", version: "1.0.0" }]);
  await templates.show();
  await expect(templates.libraryList.locator(".tp-template")).toHaveCount(2);
  const normal = templates.entry("test.normal");
  await expect(normal).toContainText("Flat colors where a frame's art goes");
  // The catalog says which layouts each one renders, before either is installed
  await expect(normal.locator(".tp-chip")).toHaveText(["Standard"]);
  await expect(templates.entry("test.transform").locator(".tp-chip")).toHaveText(["Transform"]);
  await expect(normal.getByRole("button", { name: /^Download · \d+ KB$/ })).toBeVisible();

  await normal.getByRole("button", { name: "Download" }).click();
  await expect(templates.label).toHaveText("Test frame · 1.0.0");
  await expect(normal).toContainText("active");
  await expect(normal.locator(".tp-chip.on")).toHaveText(["Standard"]);

  // The bundle is a real template, so a card renders with it
  await single.show();
  await single.search("sol ring");
  await single.select("Sol Ring");
  await single.render();
  await expect(single.preview).toBeVisible();
  expect((await fake.requests()).some((r) => r.endsWith(".mimic"))).toBe(true);
});

test("a template for transform cards makes them supported", async ({ templates, list, fake }) => {
  await fake.catalog([{ name: "test.normal", version: "1.0.0" }, { name: "test.transform", version: "1.0.0" }]);
  await list.paste("1 Delver of Secrets");
  await list.resolve();
  await expect(list.row("Delver of Secrets")).toContainText("unsupported");

  await templates.show();
  await templates.entry("test.transform").getByRole("button", { name: "Download" }).click();
  await expect(templates.entry("test.transform")).toContainText("installed");

  await list.show();
  await list.paste("1 Delver of Secrets");
  await list.resolve();
  await expect(list.row("Delver of Secrets")).toContainText("matched");
});

test("an installed template offers the choices it declares", async ({ templates, fake, page }) => {
  await fake.catalog([{ name: "test.featured", version: "1.0.0", features: true }]);
  await templates.show();
  const entry = templates.entry("test.featured");
  await expect(entry.locator(".tp-feature")).toHaveCount(0);

  await entry.getByRole("button", { name: "Download" }).click();
  await expect(entry).toContainText("active");
  await expect(entry.locator(".tp-feature")).toHaveCount(3);
  await expect(entry).toContainText("Test frame");

  // A choice is kept as soon as it is made, and is still there on a reload of the list
  await entry.getByLabel("Banner").check();
  await entry.getByLabel("Tone").selectOption("dark");
  await templates.showDefaults();
  await page.locator("#tm-tab-library").click();
  await expect(templates.entry("test.featured").getByLabel("Banner")).toBeChecked();
  await expect(templates.entry("test.featured").getByLabel("Tone")).toHaveValue("dark");
});

test("a template exported from the editor is imported from its file, listed as installed, and removed again", async ({ editor, templates, scratch }) => {
  await editor.create("Portable", 100);
  await editor.exportButton.click();
  await editor.exportConfirm.click();
  await expect(editor.toasts.last()).toContainText("Saved user.portable-0.1.0.mimic");
  // The file the export wrote is the one an open dialog is given
  await scratch.offer("user.portable-0.1.0.mimic", { path: scratch.savedPath("user.portable-0.1.0.mimic") });

  await templates.show();
  await expect(templates.libraryList.locator(".tp-template", { hasText: "user.portable" })).toHaveCount(0);
  await editor.page.locator("#tm-import-template").click();
  await expect(editor.toasts.last()).toHaveText("Imported Portable 0.1.0");
  const entry = templates.entry("Portable");
  await expect(entry).toContainText("user.portable");
  await expect(entry.getByRole("button", { name: "Select" })).toBeVisible();

  // The same file again changes nothing, and the template can be selected like any other
  await editor.page.locator("#tm-import-template").click();
  await expect(editor.toasts.last()).toHaveText("Portable 0.1.0 is installed already");
  await entry.getByRole("button", { name: "Select" }).click();
  await expect(templates.label).toHaveText("Portable · 0.1.0");

  // Removing it asks first, takes the bundle out of the library and out of use, and the editor's
  // draft is untouched
  await scratch.decline();
  await entry.getByRole("button", { name: "Remove Portable 0.1.0" }).click();
  await expect(entry).toBeVisible();
  await scratch.reset();
  await entry.getByRole("button", { name: "Remove Portable 0.1.0" }).click();
  await expect(editor.toasts.last()).toHaveText("Removed Portable 0.1.0");
  await expect(templates.libraryList.locator(".tp-template", { hasText: "user.portable" })).toHaveCount(0);
  await expect(templates.label).toHaveText("No template installed");
  await editor.show();
  await expect(editor.page.locator("#ed-draft-select")).toContainText("Portable");
  await scratch.reset();
});

test("a Proxyshop PSD is converted, installed and listed, and can be removed like any other", async ({ templates, scratch, page }) => {
  await scratch.offer("normal.psd", { path: path.resolve(import.meta.dirname, "../../../testassets/proxyshop/normal.psd") });
  await templates.show();
  await page.locator("#tm-import-psd").click();

  const status = page.locator("#tm-import-status");
  await expect(status.locator(".tm-import-text")).toHaveText("Imported Normal (Proxyshop) 0.1.0. It is marked in the list below.", { timeout: 60_000 });
  const entry = templates.entry("Normal (Proxyshop)");
  await expect(entry).toHaveClass(/tp-new/);
  await expect(entry).toContainText("proxyshop.normal");
  await expect(entry).toContainText("Converted from the Proxyshop template normal.psd");
  await expect(entry.getByRole("button", { name: "Select" })).toBeVisible();
  expect((await scratch.events()).filter(([kind]) => kind === "open").map(([, p]) => path.basename(p))).toEqual(["normal.psd"]);

  // A second import keeps the first and installs the next version beside it
  await page.locator("#tm-import-psd").click();
  await expect(status.locator(".tm-import-text")).toHaveText("Imported Normal (Proxyshop) 0.1.1. It is marked in the list below.", { timeout: 60_000 });
  await expect(entry.locator(".tp-label")).toHaveText(["0.1.1", "0.1.0"]);

  for (const v of ["0.1.1", "0.1.0"]) {
    await entry.getByRole("button", { name: `Remove Normal (Proxyshop) ${v}` }).click();
    await expect(page.locator("#toasts .toast").last()).toHaveText(`Removed Normal (Proxyshop) ${v}`);
  }
  await expect(templates.libraryList.locator(".tp-template", { hasText: "proxyshop.normal" })).toHaveCount(0);
});
