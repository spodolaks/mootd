// Jest setup — runs after the test framework is installed,
// before each test file. Use for mocks of native modules that
// can't load in the jest-expo node environment.

// AsyncStorage's native module can't load under jest-expo.
// The library ships a jest-friendly in-memory mock; wire it
// here so any `import AsyncStorage from '...'` in app code
// gets the mock without each test file rolling its own.
jest.mock('@react-native-async-storage/async-storage', () =>
  require('@react-native-async-storage/async-storage/jest/async-storage-mock'),
);

// NetInfo's native module ditto. Tests that rely on
// connectivity transitions can mock the listener directly;
// this default returns "connected" so a passive consumer
// doesn't false-positive offline.
jest.mock('@react-native-community/netinfo', () => ({
  __esModule: true,
  default: {
    addEventListener: jest.fn(() => () => {}),
    fetch: jest.fn(() => Promise.resolve({ isConnected: true, type: 'wifi' })),
  },
  addEventListener: jest.fn(() => () => {}),
  fetch: jest.fn(() => Promise.resolve({ isConnected: true, type: 'wifi' })),
}));
