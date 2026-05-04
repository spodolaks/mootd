import { defineConfig } from '@hey-api/openapi-ts';

/**
 * @hey-api/openapi-ts config — generates TypeScript types from
 * the vendored user-api spec (mootd-admin#41).
 *
 * Output is types-only (no client). Our existing apiClient in
 * `src/data/api/client.ts` owns the request/refresh/retry
 * behaviour and we're not swapping it for the generated client;
 * we just want the generated **shapes** so frontend types can
 * be imported alongside the hand-written domain models in
 * `src/domain/models/` until the migration finishes.
 *
 * Regenerate: `npm run gen`. Drift-check in CI:
 * `npm run gen:check`.
 *
 * Migration plan (follow-up work):
 *   - Replace each hand-written type in src/domain/models/ with
 *     a re-export from generated/types.gen.ts.
 *   - Drop the hand-written files once every consumer imports
 *     from the generated module.
 *
 * Until that finishes, the hand-written types are still
 * authoritative — generated types are reference + drift-detection
 * (same posture as backend/internal/usergen/).
 */
export default defineConfig({
  input: './contracts/user-api.yaml',
  output: './src/data/api/generated',
  plugins: ['@hey-api/typescript'],
});
