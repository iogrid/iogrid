// Jest config for mobile/ios — minimal pure-JS / node-env setup.
//
// Goal: exercise the pure-TS modules under src/lib (wallet deeplink
// builders + grid_balance RPC code) WITHOUT pulling in the full
// react-native / expo-runtime test stack (jest-expo, metro-babel,
// react-native preset). Those presets weigh ~150 MiB combined and we
// only need to test deeplink string construction + fetch-based RPC
// edge cases. Native-shim modules (expo-linking, expo-crypto,
// expo-secure-store, expo-router) are mocked via `moduleNameMapper`
// in src/lib/mocks/.
//
// Run with `npx jest --config jest.config.js` from mobile/ios/.
// The harness uses ts-jest's default ESM-aware TS transform with
// `isolatedModules: true` so the tests run without needing the full
// @types/react RN ambient declarations to resolve.

/** @type {import('jest').Config} */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['<rootDir>/src/**/__tests__/**/*.test.ts', '<rootDir>/src/**/__tests__/**/*.test.tsx'],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json'],
  transform: {
    '^.+\\.tsx?$': [
      'ts-jest',
      {
        diagnostics: false,
        tsconfig: {
          module: 'commonjs',
          target: 'es2020',
          esModuleInterop: true,
          allowSyntheticDefaultImports: true,
          isolatedModules: true,
          strict: false,
          skipLibCheck: true,
          jsx: 'react',
          types: [],
        },
      },
    ],
  },
  // Native-shim modules don't load under node — redirect to the local
  // mocks in src/lib/mocks/. These also serve as the runtime contract
  // tests assert against (e.g. capturing the openURL() argument).
  moduleNameMapper: {
    '^expo-linking$': '<rootDir>/src/lib/mocks/expo-linking.ts',
    '^expo-crypto$': '<rootDir>/src/lib/mocks/expo-crypto.ts',
    '^expo-router$': '<rootDir>/src/lib/mocks/expo-router.ts',
    '^@react-native-async-storage/async-storage$':
      '<rootDir>/src/lib/mocks/async-storage.ts',
    // react-native + its peer transitive deps don't load under
    // `testEnvironment: node`. Stub them so modules under test
    // (e.g. src/app/_layout.tsx, onboarding screens) can import
    // without crashing on TurboModule access.
    '^react-native$': '<rootDir>/src/lib/mocks/react-native.ts',
    '^react-native-reanimated$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^react-native-worklets$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^react-native-safe-area-context$':
      '<rootDir>/src/lib/mocks/empty-module.ts',
    '^react-native-gesture-handler$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^react-native-screens$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^expo-image$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^expo-status-bar$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^expo-splash-screen$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^expo-font$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^expo-constants$': '<rootDir>/src/lib/mocks/empty-module.ts',
    // CSS / asset imports are noise under node-tests.
    '\\.(css|less|scss)$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '\\.(png|jpg|jpeg|gif|svg)$': '<rootDir>/src/lib/mocks/empty-module.ts',
    // Resolve `@/...` path alias the same way the production tsconfig
    // does (relative to src/). The CSS-suffix rule above must take
    // precedence — Jest evaluates moduleNameMapper entries in
    // declaration order, so we list `@/global.css` redirect explicitly
    // here in case the regex precedence trips.
    '^@/global\\.css$': '<rootDir>/src/lib/mocks/empty-module.ts',
    '^@/(.*)$': '<rootDir>/src/$1',
  },
  // Default 5s — bump because the retry-with-backoff test waits for
  // setTimeout(0) batches.
  testTimeout: 10000,
  clearMocks: true,
};
