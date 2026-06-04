# MOOTD — Review Follow-ups

Tracked follow-ups from an architecture + UI/UX review of the app (frontend UI/UX,
moodboard/outfit generation, backend structure), 2026-06-04. Every item was verified
against the source; locations are given as `path:line` (approximate — they drift as code
changes).

## Already shipped

- **PR #124** — frontend UI/UX quick wins: global `ToastHost` (toasts were silent no-ops),
  bounded the moodboard generation poll loop, theme-aware `LoadingOverlay`, fixed the
  `Inter-Regular` calendar font, the hardcoded "black {category}" detection title, and
  shipped placeholder copy.
- **PR #125** — corrected the backend architecture docs (CLAUDE.md ×2 + README) to match
  the real ~20-package codebase, middleware chain, generator interface, job statuses, and
  the false "CI runs `make gen-check`" claim.

## Open follow-ups

Priority key: **P1** = correctness/cost/reliability, **P2** = quality/maintainability.

---

### 1. Outfit generation path bypasses the apiClient auth-refresh interceptor — **P1**

`streamOutfitGeneration` (`app/src/data/repositories/wardrobe/WardrobeRepository.api.ts:~322`)
and `getOutfits` (`~419`) use raw `fetch` with a manually-attached `Authorization` header
instead of `apiClient`. So the 401 silent-refresh interceptor never fires on the generation
path: with 15-minute access tokens, the first Generate after expiry fails and is swallowed
by the sync fallback in `MoodBoardScreen`. The friendly 429 message is lost the same way
(the SSE path throws `stream open failed: HTTP 429` and falls into the silent fallback).

**Fix:** route these through `apiClient` (or replicate its 401-refresh + 429 mapping for the
streaming/fetch paths). Also add a client-side timeout on the SSE read loop.

---

### 2. Archetype-default fillers bypass the outfit result cache (LLM cost leak) — **P1**

`buildCacheKey` (`backend/internal/outfit/service.go:~1118`, called at `~588`) hashes the
item IDs in the generation pool, which includes archetype-default "filler" items. Fillers are
randomly re-sampled at the DB layer (`$sample`, mootd#72) on every call, so the key changes
every time — the comment at `service.go:~544` admits the cache is "effectively bypassed when
fillers are in play." Users with small wardrobes get the most fillers and are the cold-start
majority, so they essentially never hit the 24h cache and re-pay the LLM on every Generate.

**Fix:** exclude fillers from the cache key, or cache the sampled filler set per (user, day),
or seed `$sample` deterministically per day.

---

### 3. Moodboard generation: add progress + cancel during the wait — **P2**

During generation `MoodBoardScreen` shows a single `ActivityIndicator` + text
(`MoodBoardScreen.tsx:~498`). Progress messages only arrive on the SSE path; the poll-fallback
path shows a static "Generating outfits..." for the full 5–30s. No progress bar, elapsed time,
or cancel button. The JSON/poll path also keeps generating server-side if the user leaves
(no cancel endpoint; the SSE path does abort on disconnect).

**Fix:** add a Cancel affordance + elapsed/staged progress on the poll-fallback path.
(PR #124 bounded the poll loop — the reliability half; this is the UX half.)

---

### 4. Surface silent load/save failures in the UI — **P2**

Several screens swallow errors so a failure is indistinguishable from a no-op:
- `CalendarScreen.tsx:~70` and `StyleAnalysisScreen.tsx:~280` swallow load errors with no UI.
- `ItemDetailsScreen.tsx:~81,~96` only `console.error` on save/delete failure.

**Fix:** now that a global toast host exists (PR #124), route these through
`showToast(..., 'error')` or an inline error state.

---

### 5. Harden parseLLMResponse for the Ollama/OpenAI generators — **P2**

`parseLLMResponse` (`backend/internal/outfit/domain.go:~82`, used by the Ollama and OpenAI
generators) has no markdown-fence stripping and no "extract the first balanced JSON object
from prose" logic. It relies entirely on the provider honouring JSON mode; any code fence or
surrounding prose makes the top-level `json.Unmarshal` fail and dumps the whole response to
the deterministic fallback. It also swallows every unmarshal error and embeds the full raw
response in the error string.

**Fix:** strip code fences + extract the first balanced JSON object before unmarshalling.
(Claude is immune — it uses tool-use with an item-ID enum.)

---

### 6. Accessibility sweep to meet the project's own mootd#52 mandate — **P2**

CLAUDE.md (mootd#52) requires `testID` + `accessibilityLabel` on every interactive surface;
several core controls miss role/state:
- `GradientButton` and `Button` set `disabled` but no `accessibilityRole="button"` /
  `accessibilityState={{disabled}}`.
- Custom `Toggle` (permission switches) has no `accessibilityRole="switch"` / `checked`.
- `SegmentedControl` segments have no role/state/label.
- Screens with ~0 a11y on interactive elements: Profile, Preferences, Permissions,
  BuildWardrobe, DetectedItem, StyleAnalysis.
- No Dynamic Type support anywhere (no `allowFontScaling`/`maxFontSizeMultiplier`); many
  fixed-height controls clip text at large OS font sizes.

---

### 7. Wire `make gen-check` (and frontend `gen:check`) into CI — **P2**

`backend/CLAUDE.md` previously claimed CI runs `make gen-check` — there is no such step in
`.github/` (corrected in PR #125), so OpenAPI→Go codegen drift is unenforced and
`internal/admin/gen/types.go` / `internal/usergen/types.go` can silently diverge from the
vendored contracts.

**Fix:** add `make gen-check` to the backend CI job and `npm run gen:check` to the frontend
job so spec/codegen drift fails the build.

---

### 8. Decompose app.NewHTTPHandler (1596 lines, untested) and consolidate config — **P2**

`internal/app/app.go`'s `NewHTTPHandler` is ~1596 lines wiring ~20 domains with zero test
coverage — the most complex code in the repo. It also reads ~25 runtime toggles via raw
`os.Getenv` (e.g. `TRUSTED_PROXY_CIDRS`, `DETECTION_BACKEND`, `SINGLEITEM_*`, `FLATLAY_*`,
`SMTP_*`, `ADMIN_ALLOWED_IPS`, `MIN_CLIENT_VERSION`, `MAINTENANCE`, `OUTFIT_CRITIC_ENABLED`)
instead of the typed `config.Config`, so `Config` is no longer the single source of truth.

**Fix:** split per-domain wiring into `wireAdmin`/`wireOutfit`/`wireWardrobe`/… helpers, move
the `os.Getenv` reads into `config.Config`, and add coverage for the wiring assembly.

---

### 9. JWT hardening: validate iss, detect refresh-token reuse, extract refresh-expiry constant — **P1**

- `shared/jwt.ValidateToken` (`backend/internal/shared/jwt/jwt.go:~53`) checks the signing
  method but does **not** validate `iss == "mootd"` (the admin validator does —
  `admin/jwt.go:~66`). Add issuer validation for parity/defense-in-depth.
- The refresh flow (`auth/handler.go` `Refresh ~188`) has **no reuse detection** — a
  stolen+reused token just fails silently instead of triggering chain revocation, and rotation
  (`FindByRefreshToken` → `SaveRefreshToken`) is non-atomic. Add revoke-on-reuse + ordered
  rotation.
- Refresh expiry is hardcoded inline 3× as `30*24*time.Hour` (`auth/handler.go:76,157,235`) —
  extract a `config.DefaultRefreshExpiry` constant (mirrors `config.DefaultJWTExpiry`).
- Add handler-level tests for refresh/rotation/logout.

---

### 10. Standardize the typed error envelope across handlers — **P2**

A typed system exists — `response.WriteJSONErrWithCode` emits `{error, code, requestId}` with
stable `ErrorCode` constants — but only ~3 files use it. ~29 handlers still return raw
`map[string]string{"error": ...}` (no `code`/`requestId`) and ~35 use plain-text `http.Error`
(including every method-guard), which breaks the "all errors are JSON `{error}`" contract.

**Fix:** adopt `WriteJSONErrWithCode` across handlers and replace plain-text `http.Error`
guards with the JSON envelope so clients can switch on `code`.

---

_Generated as part of a Claude Code review. Verified against source; not auto-applied._
