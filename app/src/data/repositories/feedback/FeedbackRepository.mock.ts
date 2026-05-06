import type { FeedbackSubmitRequest, IFeedbackRepository } from '@/src/domain';

/** In-memory feedback repository for local/offline dev. Logs the event to the
 *  console so the rating + swap UI can be exercised without a backend. */
export class MockFeedbackRepository implements IFeedbackRepository {
  async submit(req: FeedbackSubmitRequest): Promise<void> {
     
    console.log('[mock feedback]', req);
  }
}
