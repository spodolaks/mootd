import type { FeedbackSubmitRequest, IFeedbackRepository } from '@/src/domain';
import { apiClient } from '@/src/data/api/client';

/** Backend-backed feedback repository. Calls POST /v1/outfits/feedback.
 *
 *  Feedback is fire-and-forget from the UI's perspective — if the backend is
 *  down or the user is offline, we don't want to block a thumbs-up or a swap
 *  with a spinner. The repository surfaces errors to the caller so the store
 *  can decide to log / ignore / retry; the current call sites do the former.
 */
export class ApiFeedbackRepository implements IFeedbackRepository {
  async submit(req: FeedbackSubmitRequest): Promise<void> {
    await apiClient.post<{ id: string }>('/v1/outfits/feedback', req);
  }
}
