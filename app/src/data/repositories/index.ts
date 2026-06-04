import type {
  IAuthRepository,
  IBrandsRepository,
  IFeedbackRepository,
  IMoodBoardRepository,
  IWardrobeRepository,
} from '@/src/domain';
import { ApiAuthRepository } from './auth/AuthRepository.api';
import { MockAuthRepository } from './auth/AuthRepository.mock';
import { ApiBrandsRepository } from './brands/BrandsRepository.api';
import { MockBrandsRepository } from './brands/BrandsRepository.mock';
import { ApiWardrobeRepository } from './wardrobe/WardrobeRepository.api';
import { MockWardrobeRepository } from './wardrobe/WardrobeRepository.mock';
import { ApiMoodBoardRepository } from './moodboard/MoodBoardRepository.api';
import { MockMoodBoardRepository } from './moodboard/MoodBoardRepository.mock';
import { ApiFeedbackRepository } from './feedback/FeedbackRepository.api';
import { MockFeedbackRepository } from './feedback/FeedbackRepository.mock';

export type DataSource = 'mock' | 'api';

// Default to 'api' (production behaviour). Only flip to 'mock' when the
// developer explicitly opts in via EXPO_PUBLIC_DATA_SOURCE=mock in .env —
// so a missing/typo'd env var can never silently downgrade production to
// the in-memory mock backend.
const resolveDataSource = (): DataSource => {
  const v = process.env.EXPO_PUBLIC_DATA_SOURCE;
  return v === 'mock' ? 'mock' : 'api';
};

export const activeDataSource: DataSource = resolveDataSource();

export const authRepository: IAuthRepository =
  activeDataSource === 'api' ? new ApiAuthRepository() : new MockAuthRepository();

export const wardrobeRepository: IWardrobeRepository =
  activeDataSource === 'api' ? new ApiWardrobeRepository() : new MockWardrobeRepository();

export const brandsRepository: IBrandsRepository =
  activeDataSource === 'api' ? new ApiBrandsRepository() : new MockBrandsRepository();

export const moodBoardRepository: IMoodBoardRepository =
  activeDataSource === 'api' ? new ApiMoodBoardRepository() : new MockMoodBoardRepository();

export const feedbackRepository: IFeedbackRepository =
  activeDataSource === 'api' ? new ApiFeedbackRepository() : new MockFeedbackRepository();
