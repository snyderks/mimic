// The states of the page that the visual and accessibility tests look at. Each
// scene sets the replies it needs on a bridge, boots the page and leaves it in
// its state, with nothing in it that changes from one run to the next except
// what its mask names
import { expect } from "@playwright/test";
import { EditorPage } from "../pages/EditorPage.mjs";
import { ListPage } from "../pages/ListPage.mjs";
import { OverridesPage } from "../pages/OverridesPage.mjs";
import { SettingsDialog } from "../pages/SettingsDialog.mjs";
import { SinglePage } from "../pages/SinglePage.mjs";
import { noTemplate } from "../bridge-fixtures.mjs";
import { assetsView, at, card, describeView, done, draftSummary, draftView, partView, presetCards, result, standardAssets, standardItems } from "./cards.mjs";

const ELF = {
  ManaCost: "{G}",
  TypeLine: "Creature — Elf Druid",
  OracleText: "{T}: Add {G}.",
  FlavorText: "Roots first, then everything else.",
  Power: "1",
  Toughness: "1",
  Colors: ["G"],
  Artist: "Anson Maddocks",
  SetCode: "dom",
  CollectorNumber: "168",
};

const row = (line, text, qty = 1, extra) => ({ line, text, qty, name: text, ...extra });

// A list of rows in every state a row can be in, resolved and shown in full
async function resolvedList(bridge, page) {
  const rows = [row(1, "Lightning Bolt", 4), row(2, "Sol Ring"), row(3, "Llanowar Elves", 2), row(4, "Delver of Secrets"), row(5, "Nonexistent Card", 3, { group: "Sideboard" })];
  const bolt = card("Lightning Bolt", { ManaCost: "{R}", TypeLine: "Instant", SetCode: "2x2", CollectorNumber: "117", Colors: ["R"] });
  const ring = card("Sol Ring", { SetCode: "c21", CollectorNumber: "263" });
  const ringAlt = card("Sol Ring", { SetCode: "cmr", CollectorNumber: "472" });
  const delver = card("Delver of Secrets // Insectile Aberration", { SetCode: "isd", CollectorNumber: "51", Layout: "transform", shapes: [{ role: "transform_front", kind: "standard" }, { role: "transform_back", kind: "standard" }] });
  bridge.reply("List.Resolve", { jobId: "r1", format: "quantity", rows });
  await bridge.boot();
  const list = new ListPage(page);
  await list.show();
  await list.paste(rows.map((r) => `${r.qty} ${r.text}`).join("\n"));
  await page.locator("#list-resolve").click();
  await expect.poll(() => bridge.calledWith("List.Resolve").length).toBe(1);
  await bridge.job("r1", [
    { seq: 1, frac: 0.2, row: { index: 0, status: "matched", card: bolt } },
    { seq: 2, frac: 0.4, row: { index: 1, status: "ambiguous", candidates: [ring, ringAlt] } },
    { seq: 3, frac: 0.6, row: { index: 2, status: "error", note: "the lookup timed out" } },
    { seq: 4, frac: 0.8, row: { index: 3, status: "matched", card: delver } },
    { seq: 5, frac: 1, row: { index: 4, status: "notFound" } },
    done(6),
  ]);
  await expect(page.locator("#list-progress")).toBeHidden();
  await list.chip("All").click();
  return list;
}

const runView = {
  id: "run-1",
  label: "Pasted list",
  outDir: "/home/user/proxies",
  template: "normal",
  dpi: 274,
  format: "jpeg",
  concurrency: 4,
  started: "2026-10-08T12:00:00Z",
  finished: "2026-10-08T12:00:07Z",
  report: "mimic-run-20261008-120000.json",
  warmupMs: 210,
  warmupCards: 4,
  cards: [
    { index: 0, name: "Lightning Bolt", status: "done", frac: 1, file: "Lightning Bolt [2X2-117].jpg", ms: 420 },
    { index: 1, name: "Sol Ring", status: "done", frac: 1, file: "Sol Ring [C21-263].jpg", ms: 390 },
    { index: 2, name: "Llanowar Elves", status: "failed", stage: "art", error: "fetching art: scryfall answered 503" },
    { index: 3, name: "Delver of Secrets", status: "unsupported", error: "No installed template renders transform front faces" },
  ],
};

// Each scene is { mask, run } where run gets the bridge and the page, and mask
// lists what a screenshot leaves out: the card image, which is a stand-in, and
// anything that shows a time
export const scenes = {
  "no-template": {
    run: async ({ bridge, page }) => {
      noTemplate(bridge);
      await bridge.boot();
      await expect(page.locator("#no-template-strip")).toBeVisible();
      await expect(page.locator("#activity-title")).toHaveText("No template is installed");
    },
  },
  "single-empty": {
    run: async ({ bridge }) => bridge.boot(),
  },
  "single-card": {
    mask: ["#stage", "#activity"],
    run: async ({ bridge, page }) => {
      bridge.reply("Cards.Search", [result("Llanowar Elves", ELF)]);
      bridge.reply("Cards.Printings", []);
      await bridge.boot();
      const single = new SinglePage(page);
      await single.search("llanowar elves");
      await single.select("Llanowar Elves");
      await expect.poll(() => bridge.calledWith("Render.Start").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(single.activity).toHaveAttribute("data-state", "done");
    },
  },
  "editor-empty": {
    run: async ({ bridge, page }) => {
      await bridge.boot();
      await new EditorPage(page).show();
      await expect(page.locator("#ed-empty")).toBeVisible();
    },
  },
  "editor-draft": {
    mask: ["#ed-stage", "#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame", { forkedFrom: "mimic.m15 1.0.0" });
      bridge.reply("Studio.List", [mine, draftSummary("older", "Older frame", { updated: "2026-10-01T09:00:00Z" })]);
      bridge.reply(
        "Studio.Open",
        draftView(mine, { diagnostics: [{ severity: "warning", path: "template.json", message: "the template has no description" }] }),
      );
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.activity).toHaveAttribute("data-state", "done");
      await expect(editor.problems).toHaveCount(1);
    },
  },
  "editor-selection": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.activity).toHaveAttribute("data-state", "done");
      await expect(editor.layers).toHaveCount(7);
      await editor.sizeCanvas(300);
      await editor.layers.filter({ hasText: "oracle" }).click();
      await expect(editor.handles).toHaveCount(8);
      // The first edit is made, so undo is there and the stand-in for the card is hidden
      await page.keyboard.press("ArrowRight");
      await expect(editor.undoButton).toBeEnabled();
    },
  },
  "editor-card-fields": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(7);
      await editor.openFine();
      // A field is changed, so the preset lets go and the form says what differs
      await editor.cardField("typeLine").fill("Land");
      await expect(page.locator("#ed-fine-revert")).toBeVisible();
    },
  },
  "editor-images": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine, { meta: { locked: ["normal/text:title"], snapTo: ["normal/text:oracle"] } }));
      bridge.assets = assetsView([partView("background", "", ["any"]), partView("frame", "background", [..."wubrg"], { problems: ["The images are not one size: 1000x1400 for w, u, b, r; 1002x1400 for g"] })]);
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(7);
      await editor.sizeCanvas(300);
      await editor.layers.filter({ hasText: "frame" }).click();
      await expect(editor.images.locator('.ed-cell[data-material="w"] img')).toHaveCount(1);
    },
  },
  "editor-add": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(7);
      await editor.addButton.click();
      await editor.addKind.selectOption("layer");
      await editor.addPart.selectOption({ label: "A new part from a PNG image" });
      await editor.addPartName.fill("glow");
      await expect(editor.addSlot).toBeVisible();
    },
  },
  "editor-export": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      bridge.reply("Studio.Release", {
        templateId: "user.my-frame",
        name: "My frame",
        version: "0.1.0",
        author: "Jane Doe",
        license: "CC-BY-4.0",
        credits: [
          { name: "Forked from mimic.m15 1.2.0", note: "frame art", url: "https://example.com/m15" },
          { name: "Jane Doe", note: "new pinlines" },
        ],
        filename: "user.my-frame-0.1.0.mimic",
        installed: false,
        active: false,
        diagnostics: [{ severity: "warning", message: "parts/frame/w.png is larger than the canvas" }],
        ready: true,
      });
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await editor.exportButton.click();
      await expect(editor.exportDialog).toBeVisible();
      await expect(editor.exportCredits).toHaveCount(2);
    },
  },
  "editor-baseline": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      const crown = { kind: "layer", key: "crown", part: "crown", drawn: false, why: 'its condition "legendary" does not hold for this card', authored: at(0, 0, 0, 0), delivered: at(0, 0, 0, 0) };
      const items = [
        standardItems[0],
        standardItems[1],
        crown,
        standardItems[2],
        { ...standardItems[3], ink: true, delivered: at(100, 100, 420, 52), authored: at(100, 100, 420, 52), baseline: at(100, 160, 600, 0), narrowed: true, placed: at(100, 80, 520, 80), override: { when: "legendary", index: 0 }, fit: { size: 30, wanted: 38, floor: 21, lines: 1, overflows: false } },
        ...standardItems.slice(4),
      ];
      bridge.source.layout.textBoxes.title.flow = { clearOf: "mana" };
      bridge.reply("Studio.Describe", () => describeView(items));
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(8);
      await editor.sizeCanvas(400);
      await editor.row("background").locator('[data-toggle="visible"]').click();
      await editor.layers.filter({ has: page.locator(".ed-layer-name", { hasText: /^title$/ }) }).click();
      await expect(page.locator("#ed-overlay .ed-box.baseline")).toHaveCount(1);
    },
  },
  "editor-coverage": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      let issued = 0;
      bridge.reply("Studio.Render", () => ({ jobId: `job-${++issued}`, dpi: 40 }));
      bridge.reply("Studio.ResolvePreset", (id) => presetCards[id]);
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await editor.coverage.locator("summary").click();
      await editor.coverageRun.click();
      for (let n = 2; n <= 7; n++) {
        await expect.poll(() => issued).toBeGreaterThanOrEqual(n);
        await bridge.job(`job-${n}`, [n === 7 ? { seq: 1, done: true, error: "no frame for a land with this condition" } : done(1)]);
      }
      await expect(editor.coverageLine).toHaveText("5 of 6 drawn. Not drawn: Land");
    },
  },
  "editor-fonts": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      const base = standardAssets();
      bridge.assets = assetsView(base.parts, {
        fonts: [
          { id: "plantin-regular", file: "font/plantin-regular.ttf", family: "Plantin", style: "Regular" },
          { id: "lost", file: "font/lost.otf", family: "Lost Face", style: "Bold", missing: true },
        ],
        roles: base.roles.map((r) => (r.role === "body" ? { ...r, font: "plantin-regular" } : r)),
      });
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(7);
      await new EditorPage(page).openSection("about");
      await expect(editor.fonts).toHaveCount(2);
      await expect(page.locator("#ed-role-body")).toHaveValue("font:plantin-regular");
    },
  },
  "editor-levers": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine, { meta: { forkedFrom: "mimic.m15 1.0.0", lockedCredits: 1, licenseLocked: true } }));
      bridge.source.template.license = "CC BY 4.0";
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await expect(editor.layers).toHaveCount(7);
      await editor.sizeCanvas(300);
      await editor.layers.filter({ hasText: "title" }).click();
      await expect(editor.lever("lever-fontSize")).toHaveValue("60");
      await editor.openCanvas();
      // The width and height are kept in proportion to begin with, so the padlock is closed
      await expect(page.locator("#ed-canvas-levers [data-ratio='lock']")).toHaveAttribute("aria-pressed", "true");
      await page.locator("#lever-shadow-on").check();
      await new EditorPage(page).openSection("about");
      await expect(editor.lever("template-license")).toHaveValue("CC BY 4.0");
      await editor.lever("lever-color").fill("red");
      await editor.lever("lever-color").blur();
      await expect(editor.leverError("lever-color")).toBeVisible();
    },
  },
  "editor-json": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const mine = draftSummary("my-frame", "My frame");
      bridge.reply("Studio.List", [mine]);
      bridge.reply("Studio.Open", draftView(mine));
      bridge.reply("Studio.CheckFile", () => ({
        writable: false,
        problems: [
          { severity: "error", line: 12, column: 5, message: "invalid character '}' looking for beginning of object key string" },
          { severity: "warning", path: "template.json", message: "the template has no description" },
        ],
      }));
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await expect.poll(() => bridge.calledWith("Studio.Render").length).toBe(1);
      await bridge.job("job-1", [done(1)]);
      await editor.jsonButton.click();
      await editor.jsonTabs.nth(1).click();
      await expect(editor.jsonProblems).toHaveCount(2);
    },
  },
  "editor-problems": {
    mask: ["#ed-activity"],
    run: async ({ bridge, page }) => {
      const broken = draftSummary("broken", "Broken frame");
      bridge.reply("Studio.List", [broken]);
      bridge.reply(
        "Studio.Open",
        draftView(broken, {
          valid: false,
          layouts: [],
          diagnostics: [
            { severity: "error", path: "layouts/normal.json", message: "unexpected end of JSON input" },
            { severity: "error", path: "layouts.normal.art[0].after", message: "names no stack entry, \"background\"" },
          ],
        }),
      );
      await bridge.boot();
      await new EditorPage(page).show();
      await expect(page.locator("#ed-problems li")).toHaveCount(2);
    },
  },
  "editor-new-dialog": {
    run: async ({ bridge, page }) => {
      await bridge.boot();
      const editor = new EditorPage(page);
      await editor.show();
      await editor.openNewDialog();
      await editor.dialogName.fill("My frame");
    },
  },
  "list-review": {
    run: async ({ bridge, page }) => void (await resolvedList(bridge, page)),
  },
  "list-inspector": {
    run: async ({ bridge, page }) => {
      const list = await resolvedList(bridge, page);
      await list.edit("Lightning Bolt");
    },
  },
  "run-console": {
    run: async ({ bridge, page }) => {
      bridge.reply("Run.Latest", runView);
      await bridge.boot();
      await page.locator('button[data-mode="run"]').click();
      await expect(page.locator("#run-state")).toHaveText("finished with failures");
      await page.locator("#run-rows > li", { hasText: "Llanowar Elves" }).click();
    },
  },
  "templates-proxyshop": {
    run: async ({ bridge, page }) => {
      const version = { version: "0.1.0", label: "0.1.0", cached: true, selectable: true, active: false, action: "select", removable: true, restorable: false };
      const m15 = { name: "mimic.m15", title: "Modern frame (M15)", description: "The modern card frame", standard: true, shapes: [{ role: "single", kind: "standard" }], versions: [{ ...version, version: "1.0.0", label: "1.0.0", active: true, action: "" }] };
      const normal = { name: "proxyshop.normal", title: "Normal (Proxyshop)", description: "Converted from the Proxyshop template normal.psd", standard: true, shapes: [{ role: "single", kind: "standard" }], versions: [version] };
      bridge.reply("Templates.List", [m15]);
      bridge.reply("System.PickFiles", ["/psd/normal.psd"]);
      bridge.reply("Templates.ImportProxyshop", "imp-1");
      await bridge.boot();
      await page.locator("#template-trigger").click();
      await expect(page.locator("#tm-library .tp-template")).toHaveCount(1);
      await page.locator("#tm-import-psd").click();
      await expect.poll(() => bridge.calledWith("Templates.ImportProxyshop").length).toBe(1);
      bridge.reply("Templates.List", [m15, normal]);
      const notes = ["normal: the layer Shadow is left out", "normal: the Nyx group draws without its glow", "the Legendary Crown group has no color for gold, so gold cards use the first"];
      await bridge.job("imp-1", [done(1, { imported: { id: "proxyshop.normal", name: "Normal (Proxyshop)", version: "0.1.0", notes } })]);
      await expect(page.locator("#tm-library .tp-template.tp-new")).toHaveCount(1);
      await page.locator("#tm-import-status summary").click();
      await expect(page.locator("#tm-import-status li")).toHaveCount(3);
      // The toast fades on a timer, so it is not part of the scene
      await page.locator("#toasts").evaluate((el) => el.replaceChildren());
    },
  },
  "templates-library": {
    run: async ({ bridge, page }) => {
      const version = (v, extra) => ({ version: v, label: v, cached: true, selectable: true, active: false, action: "select", removable: true, restorable: true, ...extra });
      bridge.reply("Templates.List", [
        {
          name: "mimic.m15",
          title: "Modern frame (M15)",
          description: "The modern card frame",
          standard: true,
          shapes: [
            { role: "single", kind: "standard" },
            { role: "split", kind: "standard" },
            { role: "transform_front", kind: "standard" },
            { role: "transform_back", kind: "standard" },
          ],
          details: {
            title: "Modern frame (M15)",
            author: { name: "mimic" },
            license: "CC BY 4.0",
            credits: [{ name: "Proxyshop", url: "https://github.com/Investigamer/Proxyshop" }],
            features: [
              { name: "tone", label: "Tone", type: "choice", values: ["light", "dark"], value: "light" },
              { name: "glow", label: "Glow", type: "bool", value: false },
              { name: "strength", label: "Strength", type: "range", min: 0, max: 1, step: 0.25, value: 0.5 },
            ],
          },
          versions: [version("1.1.0", { cached: false, action: "download", size: 235_000_000 }), version("1.0.0", { active: true, action: "" })],
        },
        {
          name: "mimic.walkers",
          title: "Walker frames",
          description: "Frames for planeswalkers",
          standard: false,
          shapes: [{ role: "single", kind: "planeswalker" }],
          versions: [version("1.0.0", { cached: false, action: "download", size: 41_300_000 })],
        },
      ]);
      // The standard and split layouts render with mimic.m15 now, and transform with another
      bridge.reply("Templates.Active", {
        name: "mimic.m15",
        version: "1.0.0",
        label: "mimic.m15 · 1.0.0",
        source: "cached",
        shapes: [{ role: "single", kind: "standard" }],
        faces: { "single/standard": "mimic.m15", "split/standard": "mimic.m15", "transform_front/standard": "other.frame" },
      });
      await bridge.boot();
      await page.locator("#template-trigger").click();
      await expect(page.locator("#tm-library .tp-template")).toHaveCount(2);
      await expect(page.locator("#tm-library .tp-name")).toHaveText(["Modern frame (M15)", "Walker frames"]);
      await expect(page.locator("#tm-library .tp-chip.on")).toHaveText(["Standard", "Split"]);
      await expect(page.locator("#tm-library .tp-feature")).toHaveCount(3);
      await expect(page.locator("#tm-library .tp-chip")).toHaveText(["Standard", "Split", "Transform", "Planeswalker"]);
      await expect(page.locator("#tm-library .tp-versions button", { hasText: "235 MB" })).toBeVisible();
    },
  },
  "overrides": {
    run: async ({ bridge, page }) => {
      bridge.reply("Overrides.Rules", [
        { id: "r1", name: "Big creatures", when: [{ field: "typeLine", op: "contains", value: "Creature" }], then: [{ op: "set", field: "power", value: "9" }] },
        { id: "r2", name: "", disabled: true, when: [], then: [{ op: "set", field: "artist", value: "Anonymous" }] },
      ]);
      await bridge.boot();
      await new OverridesPage(page).show();
      await expect(page.locator("#rules-list .rule-card")).toHaveCount(2);
    },
  },
  "settings": {
    run: async ({ bridge, page }) => {
      await bridge.boot();
      await new SettingsDialog(page).show();
      await expect(page.locator("#fonts-roles > li").first()).toBeVisible();
    },
  },
  "settings-fonts": {
    run: async ({ bridge, page }) => {
      const gotham = { name: "Gotham Medium", family: "Gotham", style: "Medium", file: "Gotham-Medium.otf", removable: true };
      const row = (role, label, own, extra) => ({ role, label, own, source: "template", family: own.split(" ")[0], ...extra });
      bridge.reply("Data.Fonts", {
        kind: "managed",
        path: "/fonts",
        writable: true,
        roles: [
          row("title", "Card name, type line and power/toughness", "Beleren Proxy Bold", { source: "override", override: "Gotham Medium", file: "Gotham-Medium.otf" }),
          row("body", "Rules text", "Plantin MT Pro Regular", { source: "template-missing-override", override: "Lost Font Regular" }),
          row("mana", "Mana and other card symbols", "Mana Regular"),
          row("saga-chapter", "Chapter text", "Plantin MT Pro Italic", { bundled: true }),
        ],
        pool: [gotham],
        rejected: [{ file: "Broken.ttf", error: "not a font file" }],
        duplicates: ["Gotham-Copy.otf"],
      });
      await bridge.boot();
      await new SettingsDialog(page).show();
      await expect(page.locator("#fonts-pool > li").first()).toBeVisible();
      // The row sits below the first screen of the dialog, so it is scrolled to
      await page.locator("#fonts").scrollIntoViewIfNeeded();
    },
  },
  "update-offer": {
    run: async ({ bridge }) => {
      bridge.reply("Updates.Status", {
        state: "available",
        current: "1.0.0",
        canInstall: true,
        release: { version: "1.1.0", notes: "Fixes", pageUrl: "https://github.com/odevine/mimic/releases/tag/ui/v1.1.0", size: 12_000_000 },
      });
      await bridge.boot();
    },
  },
};

