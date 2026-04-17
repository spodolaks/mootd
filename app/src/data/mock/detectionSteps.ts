import type { DetectionStep } from '@/src/store';

/**
 * Default detection steps used as a placeholder until real image-detection
 * results are available from the backend. Both the build-wardrobe route and
 * DetectedItemScreen fall back to this data so there is a single source of
 * truth for development/demo purposes.
 */
export const MOCK_DETECTION_STEPS: DetectionStep[] = [
  {
    category: 'blazer',
    similarItems: [
      { id: '1', label: 'Slim Fit Blazer' },
      { id: '2', label: 'Regular Blazer' },
      { id: '3', label: 'Sport Coat' },
      { id: '4', label: 'Casual Blazer' },
      { id: '5', label: 'Dinner Jacket' },
      { id: '6', label: 'Oversized Blazer' },
    ],
  },
  {
    category: 'shirt',
    similarItems: [
      { id: '1', label: 'Oxford Shirt' },
      { id: '2', label: 'Dress Shirt' },
      { id: '3', label: 'Casual Shirt' },
      { id: '4', label: 'Linen Shirt' },
      { id: '5', label: 'Polo Shirt' },
      { id: '6', label: 'Henley Shirt' },
    ],
  },
  {
    category: 'pants',
    similarItems: [
      { id: '1', label: 'Chinos' },
      { id: '2', label: 'Dress Pants' },
      { id: '3', label: 'Slim Jeans' },
      { id: '4', label: 'Cargo Pants' },
      { id: '5', label: 'Joggers' },
      { id: '6', label: 'Wide Leg Pants' },
    ],
  },
];
