// Jest mock for `react-native`.
//
// react-native pulls in TurboModule registry, Metro require-context,
// and the iOS/Android native bridges, none of which load under
// `testEnvironment: node`. The mock here surfaces just enough of the
// API for the modules-under-test (root layout, onboarding screens,
// themed components) to import without crashing.
//
// IMPORTANT: this mock makes NO behavioural promises beyond "imports
// succeed". Tests should NOT depend on Dimensions returning real
// pixel sizes or StyleSheet doing optimization — the production code
// paths the tests cover are storage + routing, not layout.

function noop(): null {
  return null;
}

export const StyleSheet = {
  create<T extends Record<string, any>>(styles: T): T {
    return styles;
  },
  flatten<T>(style: T): T {
    return style;
  },
  hairlineWidth: 1,
  absoluteFillObject: {},
};

export const Dimensions = {
  get(_kind: 'window' | 'screen') {
    return { width: 390, height: 844, scale: 3, fontScale: 1 };
  },
  addEventListener() {
    return { remove() {} };
  },
};

export const Platform = {
  OS: 'ios' as const,
  select<T>(spec: { ios?: T; android?: T; native?: T; default?: T }): T | undefined {
    return spec.ios ?? spec.native ?? spec.default;
  },
  Version: '26.0',
};

export function useColorScheme(): 'light' | 'dark' {
  return 'light';
}

export function PixelRatio() {
  return { get: () => 3, getFontScale: () => 1 };
}

// Components — render to null so any accidental React-render in a test
// doesn't blow up.
export const View = noop;
export const Text = noop;
export const Pressable = noop;
export const TouchableOpacity = noop;
export const Image = noop;
export const ScrollView = noop;
export const SafeAreaView = noop;
export const ActivityIndicator = noop;
export const Switch = noop;
export const TextInput = noop;
export const Modal = noop;
export const FlatList = noop;
export const SectionList = noop;
export const KeyboardAvoidingView = noop;
export const RefreshControl = noop;

// Animated namespace — minimal stub so `Animated.View` etc. resolve.
export const Animated = {
  View: noop,
  Text: noop,
  Image: noop,
  ScrollView: noop,
  createAnimatedComponent: (c: any) => c,
  Value: class {
    constructor(_v: number) {}
    setValue(_v: number) {}
  },
  timing: () => ({ start: (_cb?: () => void) => _cb?.() }),
  spring: () => ({ start: (_cb?: () => void) => _cb?.() }),
};

export const Easing = {
  linear: (x: number) => x,
  ease: (x: number) => x,
  inOut: (_fn: any) => (x: number) => x,
};

export const NativeModules = {} as Record<string, any>;
export const NativeEventEmitter = class {
  addListener() {
    return { remove() {} };
  }
  removeAllListeners() {}
};

export const Linking = {
  openURL: async (_url: string) => true,
  canOpenURL: async (_url: string) => true,
  addEventListener: (_e: string, _h: any) => ({ remove() {} }),
};

export const Appearance = {
  getColorScheme: () => 'light' as const,
  addChangeListener: () => ({ remove() {} }),
};

export default {
  StyleSheet,
  Dimensions,
  Platform,
  View,
  Text,
  Pressable,
  TouchableOpacity,
  Image,
  ScrollView,
  SafeAreaView,
  ActivityIndicator,
  Animated,
  Easing,
  Linking,
  Appearance,
  useColorScheme,
};
