// Jest mock for `expo-router`.
//
// expo-router pulls in react-navigation + the Expo Router runtime
// (`expo-router/_ctx`, `expo-modules-core` native bridges, Metro's
// require-context shim), none of which load under a plain `node`
// Jest environment.
//
// Tests that exercise routing decisions (the AuthGate's first-launch
// router.replace, ConnectWallet's onContinue router.replace) only
// need to capture which path was passed to `router.replace` /
// `router.push`. So we surface a jest.fn-flavoured `router` object
// here, plus a noop `Link` for any future tests that import it.
//
// Tests can reach into the mock state via __getRouter() and
// __reset() exported from this file.

type Path = string;

let replaceCalls: Path[] = [];
let pushCalls: Path[] = [];
let backCalls = 0;

export const router = {
  replace: (path: Path) => {
    replaceCalls.push(path);
  },
  push: (path: Path) => {
    pushCalls.push(path);
  },
  back: () => {
    backCalls += 1;
  },
  canGoBack: () => false,
  setParams: (_params: Record<string, unknown>) => {},
};

// Stub the navigation primitives so any module-level imports of
// these don't crash. Tests that need to render UI should use
// jest.mock() inline to override.
export function Stack(_props: any): any {
  return null;
}
Stack.Screen = function StackScreen(_props: any): any {
  return null;
};

export function Link(_props: any): any {
  return null;
}

export function useRouter() {
  return router;
}

export function useLocalSearchParams(): Record<string, string> {
  return {};
}

export function usePathname(): string {
  return '/';
}

// Theme re-exports that the root layout imports from expo-router
// (which itself re-exports from @react-navigation/native).
export const DefaultTheme = {
  dark: false,
  colors: {
    primary: '#000',
    background: '#fff',
    card: '#fff',
    text: '#000',
    border: '#ccc',
    notification: '#f00',
  },
};
export const DarkTheme = {
  dark: true,
  colors: {
    primary: '#fff',
    background: '#000',
    card: '#000',
    text: '#fff',
    border: '#333',
    notification: '#f00',
  },
};

export function ThemeProvider(_props: any): any {
  return null;
}

// -----------------------------------------------------------------------
// Test helpers (not part of the production API surface)
// -----------------------------------------------------------------------

export function __getRouter(): {
  replace: ReadonlyArray<Path>;
  push: ReadonlyArray<Path>;
  back: number;
} {
  return { replace: replaceCalls, push: pushCalls, back: backCalls };
}

export function __reset(): void {
  replaceCalls = [];
  pushCalls = [];
  backCalls = 0;
}
