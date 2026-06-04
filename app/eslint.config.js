// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

module.exports = defineConfig([
  expoConfig,
  {
    // dist/* — Expo web export. generated/** — @hey-api/openapi-ts
    // output, owned by `npm run gen`; hand-edits get clobbered on
    // regen and would break `npm run gen:check`. (Same posture as
    // golangci-lint auto-skipping backend/internal/usergen.)
    ignores: ['dist/*', 'src/data/api/generated/**'],
  },
  {
    // jest.setup.js + every test file run inside jest's
    // environment — declare globals so the linter doesn't flag
    // jest/describe/it as undefined. Flat config doesn't honour
    // the older /* eslint-env */ comments.
    files: ['jest.setup.js', '**/__tests__/**/*.{ts,tsx,js}', '**/*.test.{ts,tsx,js}'],
    languageOptions: {
      globals: {
        jest: 'readonly',
        describe: 'readonly',
        it: 'readonly',
        test: 'readonly',
        expect: 'readonly',
        beforeAll: 'readonly',
        beforeEach: 'readonly',
        afterAll: 'readonly',
        afterEach: 'readonly',
        // jest.setup.js is CommonJS (require/module). Flat config no
        // longer honours its `/* eslint-env node */` pragma, so the
        // Node globals are declared here instead.
        require: 'readonly',
        module: 'readonly',
        process: 'readonly',
        __dirname: 'readonly',
        __filename: 'readonly',
        global: 'readonly',
      },
    },
  },
]);
