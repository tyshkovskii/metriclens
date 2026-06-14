import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

// Bug/security allowlist, not a style gate — mirrors backend/.golangci.yml.
// Type-aware `strictTypeChecked` is where the real bug-catching lives
// (floating promises, misused promises, unnecessary conditions, unsafe `any`).
// Formatting is delegated to Prettier and is intentionally not enforced here.
export default tseslint.config(
  { ignores: ["dist", "coverage", "eslint.config.js"] },
  {
    files: ["src/**/*.{ts,tsx}", "vite.config.ts", "vitest.config.ts"],
    extends: [
      js.configs.recommended,
      ...tseslint.configs.strictTypeChecked,
      reactHooks.configs.flat["recommended-latest"],
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      // Enum/union switch completeness — the analogue of golangci's `exhaustive`.
      // A `default` clause counts as exhaustive, so intentional catch-alls pass
      // while a defaultless switch that drops a new variant still fails.
      "@typescript-eslint/switch-exhaustiveness-check": [
        "error",
        { considerDefaultExhaustiveForUnions: true },
      ],
      // Keep the genuinely useful part (no `[object Object]` in templates),
      // drop the noise of disallowing plain numbers/booleans.
      "@typescript-eslint/restrict-template-expressions": [
        "error",
        { allowNumber: true, allowBoolean: true },
      ],
      // Allow the `{ le: _le, ...rest }` key-strip idiom and `_`-prefixed discards.
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", ignoreRestSiblings: true },
      ],
      // Pure style, no bug/security signal — excluded like backend's stylistic linters.
      "@typescript-eslint/no-confusing-void-expression": "off",
      // Dev-server HMR hint, not a runtime risk: advise, don't gate.
      "react-refresh/only-export-components": "warn",
    },
  },
);
