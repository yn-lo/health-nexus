import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'
import vueParser from 'vue-eslint-parser'

export default [
  // Global ignores
  {
    ignores: [
      'dist/**',
      'node_modules/**',
      '*.d.ts',
      'scripts/**',
    ],
  },

  // Base JS rules
  js.configs.recommended,

  // TypeScript files
  ...tseslint.configs.recommended,

  // Vue files
  ...pluginVue.configs['flat/recommended'],

  // Vue + TypeScript override
  {
    files: ['*.vue', '**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: {
        parser: tseslint.parser,
        ecmaVersion: 'latest',
        sourceType: 'module',
      },
    },
  },

  // TypeScript: no-undef 由 tsc 处理，ESLint 无需重复检查
  {
    files: ['**/*.ts', '**/*.tsx', '**/*.vue'],
    rules: {
      'no-undef': 'off',
    },
  },

  // Custom rules
  {
    rules: {
      // TypeScript
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/explicit-function-return-type': 'off',
      '@typescript-eslint/no-non-null-assertion': 'warn',

      // Vue
      'vue/multi-word-component-names': 'off',
      'vue/no-v-html': 'warn',
      'vue/require-default-prop': 'off',
      'vue/no-setup-props-destructure': 'off',
      'vue/max-attributes-per-line': 'off',
      'vue/singleline-html-element-content-newline': 'off',
      'vue/multiline-html-element-content-newline': 'off',

      // General
      'no-console': ['error', { allow: ['warn', 'error'] }],
      'no-debugger': 'error',
      'prefer-const': 'error',
      'no-var': 'error',
    },
  },

  // Test files relaxation
  {
    files: ['**/*.test.ts', '**/*.spec.ts', 'tests/**'],
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      'no-console': 'off',
    },
  },
]
