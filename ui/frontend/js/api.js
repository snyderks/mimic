// Every call the frontend makes into the app, so the whole surface is readable
// in one place. The window exposes the Go services as bound methods, reached by
// name through the runtime Wails serves, and sends job progress as window events.
// A test on the Go side keeps the names below in step with the services

import { Call, Events } from "/wails/runtime.js";

const NS = "github.com/odevine/mimic/ui/internal/";

// rpc calls one method of a bound service, as "Cards" and "Search"
const rpc = (service, method, ...args) => Call.ByName(`${NS}desktop.${service}.${method}`, ...args);

function message(err) {
  return (err && err.message ? err.message : String(err)).trim();
}

// abortable lets an AbortSignal cancel a call that is already running. The
// promise then rejects the way an aborted fetch does, so a caller that ignores
// superseded requests by checking for AbortError keeps working
function abortable(call, signal) {
  if (!signal) return call;
  return new Promise((resolve, reject) => {
    const abort = () => {
      call.cancel();
      reject(new DOMException("The call was aborted", "AbortError"));
    };
    if (signal.aborted) return abort();
    signal.addEventListener("abort", abort, { once: true });
    call.then(resolve, (err) => reject(new Error(message(err))));
  });
}

// --- jobs ---
//
// A job reports progress as "job" window events. They are collected from the
// moment this module loads, so a job that starts reporting before its caller
// begins watching loses nothing, and each event carries a sequence number so
// the same one is never applied twice. A page that reloaded mid-job asks the app
// for what it missed

const jobs = new Map(); // id -> { seq, events, watchers }

function jobState(id) {
  let j = jobs.get(id);
  if (!j) {
    j = { seq: 0, events: [], watchers: new Set() };
    jobs.set(id, j);
  }
  return j;
}

function deliver(id, e) {
  const j = jobState(id);
  if (e.seq <= j.seq) return;
  j.seq = e.seq;
  j.events.push(e);
  for (const w of [...j.watchers]) w(e);
}

Events.On("job", (ev) => {
  const d = Array.isArray(ev.data) ? ev.data[0] : ev.data;
  if (d && d.id) for (const e of d.events || []) deliver(d.id, e);
});

// watchJob follows a job's events, calling onStep with each progress event's
// step, fraction and the whole event, and resolving with the terminal event. It
// rejects when the job reports an error. The returned promise carries close() so
// a caller can abandon a job it superseded
export function watchJob(jobId, onStep) {
  const j = jobState(jobId);
  let watcher;
  let heard = 0; // the last sequence number this watcher applied
  const p = new Promise((resolve, reject) => {
    watcher = (e) => {
      if (e.seq <= heard) return;
      heard = e.seq;
      if (e.done) {
        j.watchers.delete(watcher);
        jobs.delete(jobId);
        if (e.error) reject(new Error(e.error));
        else resolve(e);
        return;
      }
      if (onStep) onStep(e.step, e.frac, e);
    };
    j.watchers.add(watcher);
    // Events already collected, then anything the app holds beyond them. This
    // waits a tick, so a caller never hears an event before watchJob returns:
    // callers refer to the promise it returns from inside their callback
    queueMicrotask(() => {
      for (const e of [...j.events]) watcher(e);
      Call.ByName(`${NS}jobs.Service.Backlog`, jobId, j.seq).then(
        (b) => b.events.forEach((e) => deliver(jobId, e)),
        (err) => {
          if (!j.events.some((e) => e.done)) reject(new Error(message(err)));
        },
      );
    });
  });
  p.close = () => {
    j.watchers.delete(watcher);
    if (!j.watchers.size) jobs.delete(jobId);
  };
  return p;
}

// --- native dialogs and links ---

const TEXT_LISTS = [{ name: "Card lists", pattern: "*.txt;*.csv;*.dek;*.mtga" }];
const IMAGES = [{ name: "Images", pattern: "*.png;*.jpg;*.jpeg" }];
const FONTS = [{ name: "Fonts", pattern: "*.ttf;*.otf" }];
const PNGS = [{ name: "PNG images", pattern: "*.png" }];
const BUNDLES = [{ name: "mimic templates", pattern: "*.mimic" }];
const PSDS = [{ name: "Photoshop documents", pattern: "*.psd" }];

const SYSTEM = "System";

// pickFile and pickFolder resolve to a path, or an empty string when the user
// cancelled the dialog
const pickFile = (req) => rpc(SYSTEM, "PickFile", req);
const pickFolder = (req) => rpc(SYSTEM, "PickFolder", req);
// pickFiles resolves to the paths chosen, or an empty list when the user cancelled
const pickFiles = (req) => rpc(SYSTEM, "PickFiles", req);

// payload is the data of a window event, which arrives as the value itself or
// as a one-element list of it
const payload = (ev) => (Array.isArray(ev.data) ? ev.data[0] : ev.data);

export const api = {
  version: () => rpc(SYSTEM, "Version"),
  // onMenu calls back with the action of each application menu choice that the
  // page acts on: save, openList, chooseOutput or mode:<name>
  onMenu: (cb) => Events.On("menu", (ev) => cb(payload(ev))),

  // The update status is { state, current, reason, release, canInstall, written,
  // total, error }. onUpdate calls back whenever it changes
  updateStatus: () => rpc("Updates", "Status"),
  checkUpdates: () => rpc("Updates", "Check"),
  installUpdate: () => rpc("Updates", "Install"),
  restartForUpdate: () => rpc("Updates", "Restart"),
  onUpdate: (cb) => Events.On("update", (ev) => cb(payload(ev))),

  openURL: (url) => rpc(SYSTEM, "OpenURL", url),
  // confirm asks a yes or no question in a native dialog, since the webview has
  // no confirm() of its own
  confirm: (title, message, accept) => rpc(SYSTEM, "Confirm", { title, message, accept }),

  capabilities: () => rpc("Settings", "Capabilities"),
  settings: () => rpc("Settings", "Get"),
  saveSettings: (s) => rpc("Settings", "Put", s),
  // resources describes this computer's memory and what one output render costs,
  // for the concurrency setting
  // cache, when given, names a layer cache size to plan for in place of the saved one
  resources: (cache) => rpc("Settings", "Resources", cache || ""),

  search: (q, signal) => abortable(rpc("Cards", "Search", q), signal),
  recents: () => rpc("Cards", "Recents"),

  // The global override rules, in the order they apply. saveRules resolves to
  // the rules kept, or rejects naming the first rule that does not check.
  // ruleMatches counts, for each rule, how many of the rows it holds for, where
  // a row is { base, fields }. applyRules resolves to { fields, applied }, what
  // the saved rules change on a card, leaving alone the fields given as the ones
  // the user set
  rules: () => rpc("Overrides", "Rules"),
  saveRules: (list) => rpc("Overrides", "SaveRules", list),
  ruleMatches: (list, rows) => rpc("Overrides", "Matches", list, rows),
  applyRules: (base, fields) => rpc("Overrides", "Apply", base, fields || {}),
  // Presets bundle the rules and the render settings. applyPreset resolves to
  // { rules, settings }, and the page reloads what it shows from those
  presets: () => rpc("Overrides", "Presets"),
  savePreset: (name) => rpc("Overrides", "SavePreset", name),
  applyPreset: (name) => rpc("Overrides", "ApplyPreset", name),
  deletePreset: (name) => rpc("Overrides", "DeletePreset", name),
  printings: (name, signal) => abortable(rpc("Cards", "Printings", name), signal),
  symbolURL: (code, px = 36) => `/img/symbol?code=${encodeURIComponent(code)}&px=${px}`,

  // render starts a job for one target, "preview" or "output", and resolves to
  // { jobId, dpi }
  render: (base, edits, target, face) => rpc("Render", "Start", { base, edits, target, face }),
  cancelRender: (jobId) => rpc("Render", "Cancel", jobId).catch(() => {}),
  renderEvents: (jobId, onStep) => watchJob(jobId, onStep),
  renderImageURL: (jobId) => `/img/render/${jobId}?ts=${Date.now()}`,
  // saveRender asks where to put a finished render and writes it there. It
  // resolves to the path, or an empty string when the user cancelled. dir is
  // the folder the dialog opens in
  saveRender: async (jobId, dir) => {
    const name = await rpc("Render", "SuggestedFilename", jobId);
    const path = await pickFile({ title: "Save the card", name, dir: dir || "", save: true, filters: [{ name: "PNG image", pattern: "*.png" }] });
    if (!path) return "";
    await rpc("Render", "Save", jobId, path);
    return path;
  },

  resolution: () => rpc("Settings", "Resolution"),
  saveResolution: (previewDpi, outputDpi) => rpc("Settings", "SetResolution", { previewDpi, outputDpi }),

  templates: () => rpc("Templates", "List"),
  activeTemplate: () => rpc("Templates", "Active"),
  selectTemplate: async (name, version, install = false) => ({ jobId: await rpc("Templates", "Select", { name, version, install }) }),
  selectEvents: (jobId, onStep) => watchJob(jobId, onStep),
  // chooseTemplateFile asks for a .mimic bundle and resolves to its path, or an empty string when
  // the user cancelled. importTemplate installs the file at path beside the downloaded ones and
  // resolves to { template, version, title, same, replaced, exists, active }. exists says a
  // different copy of that version is installed and nothing was written, until it is sent again
  // with replace
  chooseTemplateFile: () => pickFile({ title: "Choose a template", filters: BUNDLES }),
  importTemplate: (path, replace) => rpc("Templates", "Import", { path, replace: !!replace }),
  // removeTemplate deletes an installed version from the library, and resolves to { template,
  // version, active, now }. active says it was the template in use, which the app switched away
  // from to the template named by now
  removeTemplate: (template, version) => rpc("Templates", "Remove", { template, version }),
  // importProxyshop asks for one Proxyshop PSD, or the front and back PSDs of one
  // template, and starts converting them into a template the library lists. It
  // resolves to { jobId }, or null when the user cancelled. The job's progress
  // names each part as it is drawn, and its last event carries imported, as
  // { id, name, version, notes }
  importProxyshop: async () => {
    const paths = await pickFiles({ title: "Choose a Proxyshop PSD, or the front and back of one template", filters: PSDS });
    if (!paths || !paths.length) return null;
    return { jobId: await rpc("Templates", "ImportProxyshop", paths) };
  },
  importEvents: (jobId, onStep) => watchJob(jobId, onStep),
  cancelImport: (jobId) => rpc("Templates", "CancelImport", jobId),
  // setTemplateFeature keeps the value chosen for one feature of an installed
  // template, and resolves to the template's details with it in force. A null
  // value takes the choice back
  setTemplateFeature: (template, name, value) => rpc("Templates", "SetFeature", { template, name, value }),
  faceTemplates: () => rpc("Templates", "Faces"),
  setFaceTemplate: (key, name, version) => rpc("Templates", "SetFace", { key, name, version }),

  // The editor's drafts. A draft is { id, templateId, name, updated, forkedFrom,
  // problem }. createDraft starts a blank template and forkDraft copies an
  // installed template version, each resolving to the new draft, and openDraft
  // resolves to { draft, meta, layouts, diagnostics, valid }. renderDraft starts a job
  // like render does, and resolves to { jobId, dpi }, so its events, image and cancel
  // are the render ones
  drafts: () => rpc("Studio", "List"),
  createDraft: (name, dpi) => rpc("Studio", "Create", { name, dpi: dpi || 0 }),
  forkDraft: (template, version, name) => rpc("Studio", "Fork", { template, version, name }),
  renameDraft: (id, name) => rpc("Studio", "Rename", { id, name }),
  deleteDraft: (id) => rpc("Studio", "Delete", id),
  openDraft: (id) => rpc("Studio", "Open", id),
  // testCards lists the preset test cards, each { id, label, name, set, number }, real
  // printings that are looked up when one is chosen. resolveTestCard resolves to the
  // card a preset names, as Scryfall has it, or rejects with why it could not be found
  // A test card a person saved has user: true and can be removed. saveTestCard keeps a
  // card, as the form has edited it, under a name, and resolves to its preset
  testCards: () => rpc("Studio", "Presets"),
  resolveTestCard: (id) => rpc("Studio", "ResolvePreset", id),
  saveTestCard: (label, card, edits, face) => rpc("Studio", "SavePreset", { label, card, edits: edits || null, face: face || 0 }),
  deleteTestCard: (id) => rpc("Studio", "DeletePreset", id),
  // edits are the card form's field values, as Single sends them, laid over the card by
  // the app, and may be null for none. setTestCard keeps the card as edited
  setTestCard: (id, card, edits, face) => rpc("Studio", "SetTestCard", { id, card, edits: edits || null, face: face || 0 }),
  // coverage draws the card small and without its art, for a strip of many. view is
  // { hide, show }, the items to leave out of the drawing and the layers to draw whatever
  // their condition says, each named as "layer:frame" or "text:title", for a preview that
  // looks at the template and changes none of it, and full draws at the resolution the
  // layout is authored at and not the preview's
  renderDraft: (id, card, edits, face, coverage, view) =>
    rpc("Studio", "Render", { id, card, edits: edits || null, face: face || 0, coverage: !!coverage, hide: (view && view.hide) || [], show: (view && view.show) || [], full: !!(view && view.full) }),
  // describeDraft resolves to what the draft's layout draws for the card, in draw
  // order: { layout, authored, delivered, items, editable, reason, rev }, where an
  // item is { kind, key, part, after, images, drawn, why, authored, delivered } with
  // the places as { x, y, width, height }. A text box also has baseline, the line its
  // first baseline lies on, or with anchor cap-top or cap-center the top or middle of its capitals, placed with moved and narrowed, where the card puts it,
  // override, { when, index }, and fit, { size, wanted, floor, lines, overflows }. show
  // is the layers to describe as drawn whatever their condition says
  describeDraft: (id, card, edits, face, show) => rpc("Studio", "Describe", { id, card, edits: edits || null, face: face || 0, show: show || [] }),
  // applyDraft makes edits to the draft and saves them, each { op, layout, kind, key, dx,
  // dy, rect, to } with its numbers in the finished image's pixels, and resolves to { rev,
  // inverse }, the edits that undo them. An op stretch, with a side edge of n, e, s or w and
  // a dx or a dy, moves one side of a text box
  applyDraft: (id, ops) => rpc("Studio", "Apply", { id, ops }),
  // draftSource resolves to { files: [{ name, content }] }, template.json and each
  // layout as they are on disk. checkDraftFile resolves to { writable, problems }, what
  // the text would do as a file without saving it, each problem { severity, line,
  // column, path, message }. draftVocabulary resolves to the names a field can take:
  // { draws, conditions, fontRoles, blendModes, slots }. An edit of a field is an op
  // set or unset with a kind, a key and a path of keys, and a whole file is an op
  // replace with a file and its content
  draftSource: (id) => rpc("Studio", "Source", id),
  checkDraftFile: (id, file, content) => rpc("Studio", "CheckFile", { id, file, content }),
  draftVocabulary: () => rpc("Studio", "Vocabulary"),
  // draftAssets resolves to the draft's images and fonts as { parts, fonts, roles,
  // builtins, slots }. A part is { name, slot, dir, materials, problems } with a
  // material { key, state, file, width, height, bytes, from }, and its state is
  // image, blended, any, missing or extra. draftThumbs resolves to a data URL for each
  // image path it was given. importDraftImage and importDraftFont ask for a file and
  // resolve to null when the user cancelled, and otherwise to { rev, inverse, valid,
  // diagnostics, id, warnings } like an apply does. The image goes in as the
  // material of the part, which is made with the slot when it is new. The font goes
  // in with the roles named, and its id is the result's id
  draftAssets: (id) => rpc("Studio", "Assets", id),
  draftThumbs: (id, files, edge) => rpc("Studio", "Thumbs", { id, files, edge: edge || 0 }),
  importDraftImage: async (id, part, material, slot) => {
    const path = await pickFile({ title: "Choose a PNG image", filters: PNGS });
    return path ? rpc("Studio", "ImportImage", { id, part, material, slot: slot || "", path }) : null;
  },
  importDraftFont: async (id, assign) => {
    const path = await pickFile({ title: "Choose a font", filters: FONTS });
    return path ? rpc("Studio", "ImportFont", { id, path, assign: assign || [] }) : null;
  },
  // setDraftMarks keeps which elements are locked and which ones a drag snaps to,
  // each named "layout/kind:key"
  setDraftMarks: (id, locked, snapTo) => rpc("Studio", "SetMarks", { id, locked, snapTo }),
  // releaseDraft resolves to what a bundle of the draft would say and whether it can be
  // made: { templateId, name, version, author, license, credits, filename, installed,
  // active, diagnostics, ready }. useDraft makes the draft a template the other modes
  // can use, and resolves to { template, version, replaced, active }. exportDraft asks
  // where to save a bundle and resolves to { path, bytes }, or null when the user
  // cancelled. showDraftFolder opens the draft's folder
  releaseDraft: (id) => rpc("Studio", "Release", id),
  useDraft: (id) => rpc("Studio", "Use", id),
  exportDraft: async (id, filename) => {
    const path = await pickFile({ title: "Export the template", name: filename, save: true, filters: BUNDLES });
    return path ? rpc("Studio", "Export", { id, path }) : null;
  },
  showDraftFolder: (id) => rpc("Studio", "ShowFolder", id),
  // draftStamp resolves to a fingerprint of the draft's template files, which changes when
  // anything writes them, and openDraft and applyDraft carry it as stamp
  draftStamp: (id) => rpc("Studio", "Stamp", id),

  // resolve parses a list and resolves to { jobId, format, rows }, with one
  // resolved row per event on the stream
  resolve: (text, format) => rpc("List", "Resolve", { text, format }),
  resolveEvents: (jobId, onStep) => watchJob(jobId, onStep),
  // chooseListFile asks for a list file and resolves to its path, or an empty
  // string when the user cancelled, and readListFile returns a file's text
  chooseListFile: () => pickFile({ title: "Open a card list", filters: TEXT_LISTS }),
  readListFile: (path) => rpc("List", "ReadFile", path),
  // onFilesDropped calls back with the paths of files dropped on the window
  onFilesDropped: (cb) =>
    Events.On("files-dropped", (ev) => {
      const files = Array.isArray(ev.data) && Array.isArray(ev.data[0]) ? ev.data[0] : ev.data;
      if (Array.isArray(files) && files.length) cb(files);
    }),

  // run starts a batch. mpc, { stock, foil }, makes it an MPC Autofill project
  run: (rows, outDir, label, mpc) => rpc("Run", "Start", { rows, outDir, label, mpc }),
  latestRun: () => rpc("Run", "Latest"),
  runEvents: (id, onStep) => watchJob(id, onStep),
  runImageURL: (id, n) => `/img/run/${id}/${n}`,
  stopRun: (id) => rpc("Run", "Stop", id),
  retryRun: (id) => rpc("Run", "Retry", id),
  openRunFolder: (id) => rpc("Run", "OpenFolder", id),

  // chooseFolder asks for a folder, opening in start, and resolves to its path or
  // an empty string
  chooseFolder: (start) => pickFolder({ title: "Choose the output folder", dir: start || "" }),

  mpc: () => rpc("Run", "MPCInfo"),
  // chooseCardback asks for an image and stores it as the cardback, resolving to
  // the new options, or null when the user cancelled
  chooseCardback: async () => {
    const path = await pickFile({ title: "Choose a cardback", filters: IMAGES });
    return path ? rpc("Run", "SetCardback", path) : null;
  },
  cardbackURL: () => `/img/cardback?ts=${Date.now()}`,
  // mpcFolder resolves to { existing }, the files an earlier project left in path
  mpcFolder: (path) => rpc("Run", "MPCFolder", path),

  // cardData reports the local copy of Scryfall bulk data and what a download
  // would fetch, and downloadCardData starts a download job
  cardData: () => rpc("Data", "CardData"),
  downloadCardData: async () => ({ jobId: await rpc("Data", "DownloadCardData") }),
  cardDataEvents: (jobId, onStep) => watchJob(jobId, onStep),
  removeCardData: () => rpc("Data", "RemoveCardData"),

  // fonts reports the fonts in the folder and which font each role draws with,
  // and addFont and removeFont change the app's own fonts folder, each answering
  // with the new status. addFont asks for the font file, and resolves to null when
  // the user cancelled. removeFont takes a font's "Family Style" name
  fonts: () => rpc("Data", "Fonts"),
  addFont: async () => {
    const path = await pickFile({ title: "Choose a font", filters: FONTS });
    return path ? rpc("Data", "AddFont", path) : null;
  },
  removeFont: (name) => rpc("Data", "RemoveFont", name),
  openFonts: () => rpc("Data", "OpenFonts"),
};
