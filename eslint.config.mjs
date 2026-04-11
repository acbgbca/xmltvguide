import js from '@eslint/js';
import globals from 'globals';
import unicorn from 'eslint-plugin-unicorn';
import sonarjs from 'eslint-plugin-sonarjs';

const sharedRules = {
    // ── ESLint built-in ───────────────────────────────────────────────────
    'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    'no-var': 'error',
    'prefer-const': 'error',
    'eqeqeq': ['error', 'always'],
    'no-console': ['warn', { allow: ['warn', 'error'] }],
    'no-promise-executor-return': 'error',
    'no-template-curly-in-string': 'error',

    // ── Unicorn: opinionated best practices ───────────────────────────────
    'unicorn/no-for-loop': 'error',              // prefer for..of
    'unicorn/prefer-includes': 'error',          // prefer .includes() over .indexOf()
    'unicorn/no-array-push-push': 'error',       // merge consecutive .push() calls
    'unicorn/prefer-ternary': 'warn',            // prefer ternary over simple if/else
    'unicorn/no-useless-undefined': 'error',     // no explicit `undefined` returns
    'unicorn/prefer-string-slice': 'error',      // prefer .slice() over .substr()/.substring()
    'unicorn/prefer-logical-operator-over-ternary': 'error',

    // ── SonarJS: code smells & complexity ────────────────────────────────
    'sonarjs/no-duplicate-string': ['warn', { threshold: 4 }],
    'sonarjs/cognitive-complexity': ['warn', 20],
    'sonarjs/no-identical-functions': 'error',
    'sonarjs/no-collapsible-if': 'error',
};

const sharedPlugins = {
    unicorn,
    sonarjs,
};

export default [
    // ── Base recommended rules ────────────────────────────────────────────
    js.configs.recommended,

    // ── Main app JS (ES modules, browser environment) ─────────────────────
    {
        files: ['web/js/**/*.js'],
        languageOptions: {
            ecmaVersion: 'latest',
            sourceType: 'module',
            globals: globals.browser,
        },
        plugins: sharedPlugins,
        rules: sharedRules,
    },

    // ── Service worker (classic script, ServiceWorkerGlobalScope) ─────────
    {
        files: ['web/sw.js'],
        languageOptions: {
            ecmaVersion: 'latest',
            sourceType: 'script',
            globals: {
                ...globals.browser,
                ...globals.serviceworker,
            },
        },
        plugins: sharedPlugins,
        rules: sharedRules,
    },

    // ── E2E tests (Node/CommonJS, no browser globals needed) ─────────────
    {
        files: ['e2e/**/*.ts'],
        languageOptions: {
            ecmaVersion: 'latest',
            globals: globals.node,
        },
    },
];
