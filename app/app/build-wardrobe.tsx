import { useState, useCallback, useEffect, useRef } from 'react';
import { Alert } from 'react-native';
import { useRouter } from 'expo-router';
import * as ImagePicker from 'expo-image-picker';
import { BuildWardrobeScreen } from '@/src/screens';
import { useWardrobeStore, useDetectionJobStore } from '@/src/store';
import type { DetectionStep } from '@/src/store/wardrobeStore';
import type { ClothingDetectionResult } from '@/src/domain';

/**
 * Convert API detection result into the DetectionStep[] format the wardrobe
 * store expects. Each detected item becomes one step with a single
 * "similar item" option (the detected item itself).
 */
const toDetectionSteps = (result: ClothingDetectionResult): DetectionStep[] =>
  result.items.map(item => ({
    category: item.category,
    similarItems: [
      {
        id: item.id,
        label: item.label,
        imageSource: item.pngImageUrl
          ? { uri: item.pngImageUrl }
          : item.imageUrl
            ? { uri: item.imageUrl }
            : undefined,
        hasPng: !!item.pngImageUrl,
        // Carry the detected attributes through to the review flow.
        // Without this the onboarding wizard dropped every detected
        // trait (material, color, fit, …) and showed an empty form.
        traits: item.traits,
      },
    ],
  }));

export default function BuildWardrobe() {
  const router = useRouter();
  const { initializeFlow } = useWardrobeStore();

  // mootd#163 — onboarding detection now runs through the SAME non-blocking
  // background-job path the Wardrobe tab uses (useDetectionJobStore), instead
  // of `await detectClothing(...)` behind a full-screen, uncancelable overlay
  // that could sit there for up to 4 minutes. We kick off a job, surface its
  // staged status text inline (the action buttons stay live), and let the
  // completed-job effect below drive the transition into the review wizard.
  const startJob = useDetectionJobStore(s => s.startJob);
  const jobs = useDetectionJobStore(s => s.jobs);
  const dismissJob = useDetectionJobStore(s => s.dismissJob);

  const [error, setError] = useState<string | null>(null);
  // The job this screen kicked off. We only react to (and can cancel) our own
  // job — never some pre-existing entry from a prior Wardrobe-tab add. Held in
  // a ref so the watch-effect can read the current id without re-subscribing.
  const activeJobIdRef = useRef<string | null>(null);
  // Mirror of the active job id in state so the in-progress UI re-renders when
  // a job starts/ends.
  const [activeJobId, setActiveJobId] = useState<string | null>(null);

  // The status text for our in-flight job ("Uploading photo...", "Detecting
  // clothing items...", …). Drives the inline progress label.
  const activeJob = activeJobId ? jobs.find(j => j.id === activeJobId) ?? null : null;
  const statusText = activeJob?.status === 'detecting' ? activeJob.statusText : null;
  const isLoading = !!statusText;

  /**
   * Shared handler: start a background detection job for the selected image.
   * Returns immediately — the watch-effect picks up completion/failure.
   */
  const processImage = useCallback(
    (uri: string) => {
      setError(null);
      const jobId = startJob(uri);
      activeJobIdRef.current = jobId;
      setActiveJobId(jobId);
    },
    [startJob]
  );

  // Cancel an in-flight onboarding detection. We can't reach into the store's
  // background task to abort the network request, so we drop the job and clear
  // our reference to it: the watch-effect then finds no active job, the
  // in-flight result (whenever it lands) is discarded, and the user is left
  // back on the photo-pick step.
  const handleCancelDetection = useCallback(() => {
    const jobId = activeJobIdRef.current;
    if (jobId) {
      dismissJob(jobId);
    }
    activeJobIdRef.current = null;
    setActiveJobId(null);
    setError(null);
  }, [dismissJob]);

  // Watch for OUR detection job finishing. We look our job up by id rather
  // than going through the store's consume* helpers (which return the FIRST
  // completed/failed entry — that could be a stale one that isn't ours).
  // Completed → initialize the onboarding flow and push into the review
  // wizard (mootd#161 keeps the permissions pitch + completion tail by
  // tagging the origin 'onboarding'). Failed/timed-out → surface the error
  // inline. Both branches dismiss the job so it can't re-fire on a later
  // render and clear the active-job state so the screen leaves its busy mode.
  useEffect(() => {
    const jobId = activeJobIdRef.current;
    if (!jobId) return;
    const job = jobs.find(j => j.id === jobId);
    if (!job || job.status === 'detecting') return;

    dismissJob(jobId);
    activeJobIdRef.current = null;
    setActiveJobId(null);

    if (job.status === 'completed') {
      const steps = toDetectionSteps(job.result!);
      if (steps.length === 0) {
        setError('No clothing items were detected. Try a different photo.');
        return;
      }
      // mootd#161 — mark this as the onboarding flow so trait-selection's
      // Done handler keeps the permissions pitch + completion screen.
      initializeFlow(steps, 'onboarding');
      router.push('/detected-item');
      return;
    }

    // status === 'failed'
    setError(job.error || 'Something went wrong during detection.');
  }, [jobs, dismissJob, initializeFlow, router]);

  const handleTakePhoto = useCallback(async () => {
    // Request camera permission
    const { status } = await ImagePicker.requestCameraPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Camera access is needed to take a photo of your outfit.');
      return;
    }

    const result = await ImagePicker.launchCameraAsync({
      mediaTypes: ['images'],
      // Skip the iOS square-crop editor so the detector gets the full frame.
      quality: 0.8,
    });

    if (!result.canceled && result.assets?.[0]?.uri) {
      processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleUploadFromGallery = useCallback(async () => {
    // Request media library permission
    const { status } = await ImagePicker.requestMediaLibraryPermissionsAsync();
    if (status !== 'granted') {
      Alert.alert('Permission required', 'Photo library access is needed to choose a photo.');
      return;
    }

    const result = await ImagePicker.launchImageLibraryAsync({
      mediaTypes: ['images'],
      // Same reason — keep the original resolution, skip the crop UI.
      quality: 0.8,
    });

    if (!result.canceled && result.assets?.[0]?.uri) {
      processImage(result.assets[0].uri);
    }
  }, [processImage]);

  const handleBrowseCatalog = () => {
    router.replace('/(main)/moodboard');
  };

  return (
    <BuildWardrobeScreen
      onTakePhoto={handleTakePhoto}
      onUploadFromGallery={handleUploadFromGallery}
      onBrowseCatalog={handleBrowseCatalog}
      isLoading={isLoading}
      detectionStatus={statusText}
      onCancelDetection={handleCancelDetection}
      error={error}
    />
  );
}
