// https://docs.expo.dev/guides/using-eslint/
const { defineConfig } = require('eslint/config');
const expoConfig = require('eslint-config-expo/flat');

module.exports = defineConfig([
  expoConfig,
  {
    ignores: ['dist/*'],
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
      },
    },
  },
]);
