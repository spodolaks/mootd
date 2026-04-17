export {
  usePreferencesStore,
  type PreferencesState,
  type TemperatureUnit,
  type ThemePreference,
} from './preferencesStore';
export { useUIStore } from './uiStore';
export { useAuthStore, type AuthState } from './authStore';
export {
  useWardrobeStore,
  getDefaultTraitsForCategory,
  type WardrobeItem,
  type Trait,
  type DetectionStep,
  type DetectedItemOption,
} from './wardrobeStore';
export {
  useDetectionJobStore,
  type DetectionJob,
  type DetectionJobStatus,
} from './detectionJobStore';
