import { defineConfig } from "eslint/config";

export default defineConfig([
  {
    languageOptions: {
      ecmaVersion: 6,
      sourceType: "module",
      parserOptions: {
        ecmaVersion: 'latest',
        sourceType: 'module',
        // Enable import assertions
        ecmaFeatures: {
          // This doesn't fully support it in base ESLint parser
        }
      }
    },
  },
]);
