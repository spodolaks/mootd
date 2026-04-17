import type { IAuthRepository, IBrandsRepository, IMoodBoardRepository, IWardrobeRepository } from '@/src/domain';
import { ApiAuthRepository } from './auth/AuthRepository.api';
import { MockAuthRepository } from './auth/AuthRepository.mock';
import { ApiBrandsRepository } from './brands/BrandsRepository.api';
import { MockBrandsRepository } from './brands/BrandsRepository.mock';
import { ApiWardrobeRepository } from './wardrobe/WardrobeRepository.api';
import { MockWardrobeRepository } from './wardrobe/WardrobeRepository.mock';
import { ApiMoodBoardRepository } from './moodboard/MoodBoardRepository.api';
import { MockMoodBoardRepository } from './moodboard/MoodBoardRepository.mock';

export type DataSource = 'mock' | 'api';

const resolveDataSource = (): DataSource =>
  process.env.EXPO_PUBLIC_DATA_SOURCE === 'api' ? 'api' : 'mock';

export const activeDataSource: DataSource = resolveDataSource();

export const authRepository: IAuthRepository =
  activeDataSource === 'api'
    ? new ApiAuthRepository()
    : new MockAuthRepository();

export const wardrobeRepository: IWardrobeRepository =
  activeDataSource === 'api'
    ? new ApiWardrobeRepository()
    : new MockWardrobeRepository();

export const brandsRepository: IBrandsRepository =
  activeDataSource === 'api'
    ? new ApiBrandsRepository()
    : new MockBrandsRepository();

export const moodBoardRepository: IMoodBoardRepository =
  activeDataSource === 'api'
    ? new ApiMoodBoardRepository()
    : new MockMoodBoardRepository();
