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
  /** Dismiss/remove a job by ID. */
  dismissJob: (jobId: string) => void;
  /** Check if any job is currently in progress. */
  hasActiveJob: () => boolean;
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

    set((state) => ({ jobs: [job, ...state.jobs] }));

    // Run detection in background
    void (async () => {
      try {
        set((state) => ({
          jobs: state.jobs.map((j) =>
            j.id === jobId ? { ...j, statusText: 'Detecting clothing items...' } : j
          ),
        }));

        const result = await wardrobeRepository.detectClothing(imageUri);

        set((state) => ({
          jobs: state.jobs.map((j) =>
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
      } catch (e) {
        const msg = e instanceof Error ? e.message : 'Detection failed';
        set((state) => ({
          jobs: state.jobs.map((j) =>
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
      }
    })();

    return jobId;
  },

  consumeCompleted: () => {
    const state = get();
    const completed = state.jobs.find((j) => j.status === 'completed');
    return completed ?? null;
  },

  dismissJob: (jobId: string) => {
    set((state) => ({
      jobs: state.jobs.filter((j) => j.id !== jobId),
    }));
  },

  hasActiveJob: () => {
    return get().jobs.some((j) => j.status === 'detecting');
  },
}));
