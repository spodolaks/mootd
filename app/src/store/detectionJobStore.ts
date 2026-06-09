import { create } from 'zustand';
import { wardrobeRepository } from '@/src/data/repositories';
import type { ClothingDetectionResult } from '@/src/domain';

export type DetectionJobStatus = 'detecting' | 'completed' | 'failed';

export interface DetectionJob {
  id: string;
  imageUri: string;
  status: DetectionJobStatus;
  statusText: string;
  result: ClothingDetectionResult | null;
  error: string | null;
  startedAt: number;
  completedAt: number | null;
}

interface DetectionJobState {
  jobs: DetectionJob[];
  /** Start a background detection job. Returns the job ID. */
  startJob: (imageUri: string) => string;
  /** Get the most recent completed job (if any) and clear it. */
  consumeCompleted: () => DetectionJob | null;
  /**
   * Get a failed/timed-out job (if any). Mirrors `consumeCompleted`: it only
   * reads — the caller surfaces the failure and then calls `dismissJob` so it
   * doesn't re-fire on the next render.
   */
  consumeFailed: () => DetectionJob | null;
  /** Dismiss/remove a job by ID. */
  dismissJob: (jobId: string) => void;
  /** Check if any job is currently in progress. */
  hasActiveJob: () => boolean;
  /** Clear all jobs back to the initial empty state (used on sign-out). */
  clear: () => void;
}

let _jobCounter = 0;

export const useDetectionJobStore = create<DetectionJobState>((set, get) => ({
  jobs: [],

  startJob: (imageUri: string) => {
    const jobId = `det_${Date.now()}_${++_jobCounter}`;

    const job: DetectionJob = {
      id: jobId,
      imageUri,
      status: 'detecting',
      statusText: 'Uploading image...',
      result: null,
      error: null,
      startedAt: Date.now(),
      completedAt: null,
    };

    set(state => ({ jobs: [job, ...state.jobs] }));

    // Run detection in background. Two-phase: submit once (cheap, <1 s),
    // then poll every 3 s until the server reports completed/failed. Falls
    // back to the synchronous endpoint if the async one isn't available
    // (e.g. local backend without Redis, or a mock repo that hasn't
    // implemented submit yet).
    void (async () => {
      const updateStatus = (text: string) =>
        set(state => ({
          jobs: state.jobs.map(j => (j.id === jobId ? { ...j, statusText: text } : j)),
        }));

      const markCompleted = (result: ClothingDetectionResult) =>
        set(state => ({
          jobs: state.jobs.map(j =>
            j.id === jobId
              ? {
                  ...j,
                  status: 'completed' as const,
                  statusText: `Found ${result.items.length} item${result.items.length === 1 ? '' : 's'}`,
                  result,
                  completedAt: Date.now(),
                }
              : j
          ),
        }));

      const markFailed = (msg: string) =>
        set(state => ({
          jobs: state.jobs.map(j =>
            j.id === jobId
              ? {
                  ...j,
                  status: 'failed' as const,
                  statusText: msg,
                  error: msg,
                  completedAt: Date.now(),
                }
              : j
          ),
        }));

      try {
        updateStatus('Uploading photo...');

        let serverJobId: string | null = null;
        try {
          serverJobId = await wardrobeRepository.submitDetection(imageUri);
        } catch (submitErr) {
          // Async endpoint unavailable (503) or server doesn't know the
          // route. Fall back to the sync path — works fine on local / direct
          // origin hits where the 100s CDN cap isn't a concern.
          console.warn('[detectionJob] async submit failed, falling back to sync:', submitErr);
          updateStatus('Detecting clothing items...');
          const result = await wardrobeRepository.detectClothing(imageUri);
          markCompleted(result);
          return;
        }

        updateStatus('Detecting clothing items...');

        // Poll every 3 s for up to 5 min. That's well above the detection
        // service's own 2-min internal timeout, so a normal completion lands
        // in ~30–90 s. Failure/timeout surfaces as a failed status.
        const pollIntervalMs = 3000;
        const pollDeadline = Date.now() + 5 * 60 * 1000;

        while (Date.now() < pollDeadline) {
          await new Promise(resolve => setTimeout(resolve, pollIntervalMs));
          const poll = await wardrobeRepository.pollDetectionJob(serverJobId);

          if (poll.status === 'completed') {
            const result: ClothingDetectionResult = {
              originalImageUri: imageUri,
              items: poll.items ?? [],
            };
            markCompleted(result);
            return;
          }
          if (poll.status === 'failed') {
            markFailed(poll.error || 'Detection failed');
            return;
          }
          // processing / pending — loop and wait.
        }

        markFailed('Detection timed out after 5 minutes');
      } catch (e) {
        const msg = e instanceof Error ? e.message : 'Detection failed';
        markFailed(msg);
      }
    })();

    return jobId;
  },

  consumeCompleted: () => {
    const state = get();
    const completed = state.jobs.find(j => j.status === 'completed');
    return completed ?? null;
  },

  consumeFailed: () => {
    const state = get();
    const failed = state.jobs.find(j => j.status === 'failed');
    return failed ?? null;
  },

  dismissJob: (jobId: string) => {
    set(state => ({
      jobs: state.jobs.filter(j => j.id !== jobId),
    }));
  },

  hasActiveJob: () => {
    return get().jobs.some(j => j.status === 'detecting');
  },

  clear: () => set({ jobs: [] }),
}));
