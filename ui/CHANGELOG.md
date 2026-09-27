# Changelog

## [0.9.0](https://github.com/odevine/mimic/compare/ui/v0.8.1...ui/v0.9.0) (2026-09-27)


### Features

* **ui:** add the mimic icon as the favicon and topbar mark ([e937a4f](https://github.com/odevine/mimic/commit/e937a4fb24a75aa7ebe39e0ec74c43f132905326))

## [0.8.1](https://github.com/odevine/mimic/compare/ui/v0.8.0...ui/v0.8.1) (2026-09-27)


### Code Refactoring

* **ui:** move list parsing and row resolution into internal/cardlist ([21873de](https://github.com/odevine/mimic/commit/21873ded04089c65a3786e7f9c32c5aa0455ad6f))
* **ui:** move prefs and recents into internal/prefs ([357145a](https://github.com/odevine/mimic/commit/357145af8dd6cb5bd7223d2d3e97048db79caa00))
* **ui:** move templates and rendering into internal/pipeline ([5a0b9ce](https://github.com/odevine/mimic/commit/5a0b9cebaa7789230cf749711816e82a16de8a43))
* **ui:** move the batch run executor into internal/batch ([4398de1](https://github.com/odevine/mimic/commit/4398de12792da85e6ecb80076177a5ad255b357e))
* **ui:** move the HTTP server into internal/server ([db07a9b](https://github.com/odevine/mimic/commit/db07a9b586214a7d4da14dff5c270551f01b5b29))
* **ui:** move the local card data store into internal/carddata ([158ea28](https://github.com/odevine/mimic/commit/158ea28e4f0ecad981e106d0e52c62c513c37dac))
* **ui:** move the paced Scryfall client into internal/scryfall ([e744901](https://github.com/odevine/mimic/commit/e74490132ccfde764e59c1f643527a609fef4479))
* **ui:** move the template catalog and bundle cache into internal/catalog ([9b6b09e](https://github.com/odevine/mimic/commit/9b6b09ee83ffc0b14e5df54d7ee5e854d4bff3a9))

## [0.8.0](https://github.com/odevine/mimic/compare/ui/v0.7.0...ui/v0.8.0) (2026-09-27)


### Features

* **ui:** choose the standard template in settings and redraw the gear ([3878e4e](https://github.com/odevine/mimic/commit/3878e4e54ab57354c2115a4a146e3e82af219635))
* **ui:** manage templates in their own mode ([badbb6d](https://github.com/odevine/mimic/commit/badbb6d5f55d8886f9841b7d1f5cc358a32cddea))
* **ui:** mark the pane splitters with a grab handle ([5fac8bd](https://github.com/odevine/mimic/commit/5fac8bdc89794c6eb61e44341bb0045cf553cabe))
* **ui:** render every face through the template chosen for it ([44e27cc](https://github.com/odevine/mimic/commit/44e27ccf5199117ea85c4a7a6369e3db06b5c4c4))


### Bug Fixes

* **ui:** keep dragged panes from widening the page ([8de3da9](https://github.com/odevine/mimic/commit/8de3da90a8db288394e333ccf44e7dd65543c377))
* **ui:** keep the run preview inside its own section ([a18c7ee](https://github.com/odevine/mimic/commit/a18c7ee05e820a0d03fcd8381433c2e9512d20bd))
* **ui:** keep the single-card panes and preview within their bounds ([6898748](https://github.com/odevine/mimic/commit/689874874c42a99df11da9f3b31b9ebd54870bff))


### Build System

* **ui:** depend on the released engine v0.10.0 ([8354ca4](https://github.com/odevine/mimic/commit/8354ca42b28ee583f7f880d0c7c4fde0a03c0644))
* **ui:** depend on the released engine v0.7.0 ([8650aa0](https://github.com/odevine/mimic/commit/8650aa00449365d2680b719abab018548a5d65c3))

## [0.7.0](https://github.com/odevine/mimic/compare/ui/v0.6.0...ui/v0.7.0) (2026-09-26)


### Features

* **ui:** mark cards the active template does not support ([c14197f](https://github.com/odevine/mimic/commit/c14197f3285bfbfae6bf47ff4014a58369acfeab))
* **ui:** warn in the single-card editor when a card is unsupported ([10a50ad](https://github.com/odevine/mimic/commit/10a50ade57fed9056cb942d0f81703f0f0a95167))


### Build System

* **ui:** depend on the released engine v0.8.1 ([86c09d3](https://github.com/odevine/mimic/commit/86c09d335500b4a72ae3a3f7556e91b2d2be7b4a))

## [0.6.0](https://github.com/odevine/mimic/compare/ui/v0.5.0...ui/v0.6.0) (2026-09-25)


### Features

* **ui:** render a pasted list to a folder, watched from a run console ([2a3292c](https://github.com/odevine/mimic/commit/2a3292c50221bbf1e214c24ccc24e3b6f6e15313))
* **ui:** resolve lists from a local copy of Scryfall's bulk data ([b3df74d](https://github.com/odevine/mimic/commit/b3df74d33c830edd9d4eed80d5520da919cafc92))


### Bug Fixes

* **ui:** pace Scryfall search and named lookups at two a second ([ce8d120](https://github.com/odevine/mimic/commit/ce8d120bcc7c55ee0fa098498709e6c25dbbccba))
* **ui:** refuse cross-site requests and pace Scryfall calls ([b8f603f](https://github.com/odevine/mimic/commit/b8f603ff514e12eb1e5cf9565c114c4df241801b))


### Build System

* **ui:** depend on the released engine v0.7.0 ([850911c](https://github.com/odevine/mimic/commit/850911c76f61eeb23b298abb3ae6700f6f64eee1))

## [0.5.0](https://github.com/odevine/mimic/compare/ui/v0.4.0...ui/v0.5.0) (2026-09-22)


### Features

* **ui:** rebuild the frontend as one shell with modes and gating ([17cf927](https://github.com/odevine/mimic/commit/17cf9270f1ff2d7dc6c8eb25ecb34dc27ef6df73))
* **ui:** serve feature gates, settings, printings and mana symbols ([6ff73fa](https://github.com/odevine/mimic/commit/6ff73fafc0e935d6440a313d5d9284ed1571dc83))

## [0.4.0](https://github.com/odevine/mimic/compare/ui/v0.3.0...ui/v0.4.0) (2026-09-22)


### Features

* **ui:** add preview and output resolutions ([fd38041](https://github.com/odevine/mimic/commit/fd380416ea55d7967f8676a758f86c1702196fc8))


### Build System

* **ui:** depend on the released engine v0.6.0 ([9d1448d](https://github.com/odevine/mimic/commit/9d1448daf4b8a3d42f747db744f335be439ef6f3))

## [0.3.0](https://github.com/odevine/mimic/compare/ui/v0.2.0...ui/v0.3.0) (2026-09-22)


### Features

* **ui:** show each template's description in the manager ([bfa98eb](https://github.com/odevine/mimic/commit/bfa98eb83b2ff648361148e161f3840a56dece38))


### Build System

* **ui:** depend on the released engine v0.5.0 ([f280e0b](https://github.com/odevine/mimic/commit/f280e0b59d1e718cfd60a56c4e6bc75eb408ff8c))


### Code Refactoring

* **engine:** pull template-agnostic logic out of normal ([b8dccb8](https://github.com/odevine/mimic/commit/b8dccb8f3b68d9531f3122c755bf38e031935a65))

## [0.2.0](https://github.com/odevine/mimic/compare/ui/v0.1.0...ui/v0.2.0) (2026-09-22)


### ⚠ BREAKING CHANGES

* **ui:** the ui is no longer a Fyne desktop app. It runs a local web server and opens the app in a browser, and the binary is renamed from mimic-ui to mimic.

### Features

* **ui:** replace Fyne desktop shell with a local web server ([030c615](https://github.com/odevine/mimic/commit/030c6158c114111506179067d38b9bd4f4a2a126))

## 0.1.0 (2026-09-22)


### Features

* **ui:** add a desktop app for search, edit, and preview ([fe0d28a](https://github.com/odevine/mimic/commit/fe0d28ae63531b51ba14f33b921199106634ef4b))
* **ui:** download bundles and select templates ([a1dc0e6](https://github.com/odevine/mimic/commit/a1dc0e6015a93d19a79a813390dde80d91de01bd))
* **ui:** show a render progress bar with step status ([c56e7e9](https://github.com/odevine/mimic/commit/c56e7e9983ede2a92d0ceb6c1f85983b212f9f3c))


### Miscellaneous Chores

* **ui:** release the first version as 0.1.0 ([4007567](https://github.com/odevine/mimic/commit/4007567b94c83eb3679caf55760e5f3b2bccb717))
