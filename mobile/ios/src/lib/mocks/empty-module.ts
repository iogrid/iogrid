// Generic empty-module mock for native-bridge packages that have no
// usable surface under Jest's node environment but DO appear in
// transitive import chains of the modules under test.
//
// Every export is a no-op or a Proxy that returns more no-ops, so
// `import X from 'pkg'` / `import { Y } from 'pkg'` both resolve.

function noop(): any {
  return null;
}

const handler: ProxyHandler<any> = {
  get: (target, prop) => {
    if (prop in target) return (target as any)[prop];
    if (prop === '__esModule') return true;
    // Anything else — return a callable no-op proxy so chained
    // accesses (`Animated.View`, `Easing.inOut`, `Keyframe`) all work.
    return new Proxy(noop, handler);
  },
  apply: () => null,
  construct: () => ({}),
};

const stub = new Proxy(noop, handler);

export default stub;
export const Easing = stub;
export const Keyframe = stub;
export const scheduleOnRN = stub;
export const SafeAreaView = noop;
export const SafeAreaProvider = noop;
export const useSafeAreaInsets = () => ({ top: 0, bottom: 0, left: 0, right: 0 });

// `module.exports = stub` semantics — anyone doing
// `require('pkg').something` gets a callable proxy back.
module.exports = stub;
module.exports.default = stub;
module.exports.Easing = stub;
module.exports.Keyframe = stub;
module.exports.scheduleOnRN = stub;
module.exports.SafeAreaView = noop;
module.exports.SafeAreaProvider = noop;
module.exports.useSafeAreaInsets = () => ({ top: 0, bottom: 0, left: 0, right: 0 });
