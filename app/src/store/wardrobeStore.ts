import { create } from 'zustand';
import type { ImageSourcePropType } from 'react-native';

export interface Trait {
  id: string;
  name: string;
  selectedValue: string | null;
  options: string[];
}

export interface WardrobeItem {
  id: string;
  category: string;
  label: string;
  imageSource?: ImageSourcePropType;
  /** Set when item was selected from a brand search result; used to update imageUrl on save. */
  productImageUrl?: string;
  traits: Trait[];
}

export interface DetectedItemOption {
  id: string;
  label: string;
  imageSource?: ImageSourcePropType;
  hasPng?: boolean;
  traits?: Record<string, string>;
}

export interface DetectionStep {
  category: string;
  similarItems: DetectedItemOption[];
}

// Where the detection → review wizard was started from. Onboarding (the
// build-wardrobe screen) finishes with the "get notified" permissions pitch
// and a completion screen; an in-app add from the Wardrobe tab should skip
// that onboarding tail and drop straight back into the wardrobe (mootd#161).
export type FlowOrigin = 'onboarding' | 'add';

interface WardrobeState {
  // Detection flow configuration
  detectionSteps: DetectionStep[];
  // Current step index in the overall flow
  currentStepIndex: number;
  // Items stored by step index - persists when navigating back/forth
  items: Record<number, WardrobeItem>;
  // Where this wizard run was started (drives the Done-handler branch).
  flowOrigin: FlowOrigin;

  // Actions
  initializeFlow: (steps: DetectionStep[], origin?: FlowOrigin) => void;
  setItemForStep: (stepIndex: number, item: WardrobeItem) => void;
  setTraitValue: (traitId: string, value: string) => void;
  nextStep: () => void;
  previousStep: () => void;
  getCurrentItem: () => WardrobeItem | null;
  getItemForStep: (stepIndex: number) => WardrobeItem | null;
  getTotalSteps: () => number;
  getCurrentDetectionStep: () => DetectionStep | null;
  isLastStep: () => boolean;
  isFirstStep: () => boolean;
  getAllItems: () => WardrobeItem[];
  reset: () => void;
}

// Default trait options for each category
const DEFAULT_TRAIT_OPTIONS: Record<string, Trait[]> = {
  blazer: [
    {
      id: 'fit',
      name: 'Fit',
      selectedValue: null,
      options: ['Slim Fit', 'Regular Fit', 'Relaxed Fit', 'Oversized'],
    },
    {
      id: 'material',
      name: 'Material',
      selectedValue: null,
      options: ['Wool', 'Cotton', 'Linen', 'Polyester', 'Velvet'],
    },
    {
      id: 'style',
      name: 'Style',
      selectedValue: null,
      options: ['Single-Breasted', 'Double-Breasted', 'Casual', 'Formal'],
    },
    {
      id: 'color',
      name: 'Color',
      selectedValue: null,
      options: ['Black', 'Navy', 'Gray', 'Beige', 'Brown'],
    },
    {
      id: 'occasion',
      name: 'Occasion',
      selectedValue: null,
      options: ['Business', 'Casual', 'Formal Event', 'Smart Casual'],
    },
  ],
  shirt: [
    {
      id: 'fit',
      name: 'Fit',
      selectedValue: null,
      options: ['Slim Fit', 'Regular Fit', 'Relaxed Fit', 'Oversized'],
    },
    {
      id: 'material',
      name: 'Material',
      selectedValue: null,
      options: ['Cotton', 'Linen', 'Silk', 'Polyester', 'Oxford'],
    },
    {
      id: 'collar',
      name: 'Collar',
      selectedValue: null,
      options: ['Point', 'Spread', 'Button-Down', 'Mandarin', 'Band'],
    },
    {
      id: 'pattern',
      name: 'Pattern',
      selectedValue: null,
      options: ['Solid', 'Striped', 'Checkered', 'Plaid', 'Print'],
    },
    {
      id: 'sleeve',
      name: 'Sleeve',
      selectedValue: null,
      options: ['Long Sleeve', 'Short Sleeve', 'Roll-Up', '3/4 Sleeve'],
    },
  ],
  pants: [
    {
      id: 'fit',
      name: 'Fit',
      selectedValue: null,
      options: ['Slim Fit', 'Regular Fit', 'Relaxed Fit', 'Wide Leg', 'Tapered'],
    },
    {
      id: 'material',
      name: 'Material',
      selectedValue: null,
      options: ['Cotton', 'Wool', 'Denim', 'Linen', 'Polyester'],
    },
    {
      id: 'rise',
      name: 'Rise',
      selectedValue: null,
      options: ['Low Rise', 'Mid Rise', 'High Rise'],
    },
    {
      id: 'style',
      name: 'Style',
      selectedValue: null,
      options: ['Chinos', 'Dress Pants', 'Jeans', 'Cargo', 'Joggers'],
    },
    {
      id: 'length',
      name: 'Length',
      selectedValue: null,
      options: ['Full Length', 'Cropped', 'Ankle Length'],
    },
  ],
  // Default traits for unknown categories
  default: [
    {
      id: 'fit',
      name: 'Fit',
      selectedValue: null,
      options: ['Slim Fit', 'Regular Fit', 'Relaxed Fit', 'Oversized'],
    },
    {
      id: 'material',
      name: 'Material',
      selectedValue: null,
      options: ['Cotton', 'Polyester', 'Wool', 'Linen', 'Synthetic'],
    },
    {
      id: 'style',
      name: 'Style',
      selectedValue: null,
      options: ['Casual', 'Formal', 'Smart Casual', 'Sporty'],
    },
    {
      id: 'color',
      name: 'Color',
      selectedValue: null,
      options: ['Black', 'White', 'Navy', 'Gray', 'Other'],
    },
    {
      id: 'occasion',
      name: 'Occasion',
      selectedValue: null,
      options: ['Everyday', 'Work', 'Weekend', 'Special Event'],
    },
  ],
};

export const getDefaultTraitsForCategory = (category: string): Trait[] => {
  const normalizedCategory = category.toLowerCase();
  const traits = DEFAULT_TRAIT_OPTIONS[normalizedCategory] || DEFAULT_TRAIT_OPTIONS.default;
  // Return a deep copy to avoid mutations
  return traits.map(trait => ({ ...trait, options: [...trait.options] }));
};

// Turn a raw trait key (e.g. "color_secondary", "graphics_description")
// into a human-readable field title ("Color Secondary", "Graphics
// Description"). Used as the display-name fallback for any trait that
// isn't in the category template.
export const prettifyTraitKey = (key: string): string =>
  key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());

// buildTraitList is the single source of truth for "which trait fields to
// show for an item". It merges a flat traits map (from detection or from a
// stored wardrobe item) with the category template so that EVERY screen
// renders the same dynamic set:
//   1. one field per provided trait, in insertion order, pre-filled with
//      its value (label + options come from the category template when the
//      key matches, otherwise the key is prettified and options are empty);
//   2. then any category-template traits the map didn't already cover,
//      rendered empty so the user can still fill them in.
// Add a trait to DEFAULT_TRAIT_OPTIONS (or have the detector emit it) and it
// flows to onboarding, the item editor, and anywhere else that renders this
// list — no per-screen hardcoded key lists.
export const buildTraitList = (category: string, traits: Record<string, string>): Trait[] => {
  const defaults = getDefaultTraitsForCategory(category);
  const seen = new Set<string>();

  const fromTraits: Trait[] = Object.entries(traits).map(([key, value]) => {
    seen.add(key);
    const match = defaults.find(t => t.id === key);
    return {
      id: key,
      name: match?.name ?? prettifyTraitKey(key),
      selectedValue: value,
      options: match?.options ?? [],
    };
  });

  const missingDefaults = defaults.filter(t => !seen.has(t.id)).map(t => ({ ...t }));
  return [...fromTraits, ...missingDefaults];
};

// NOTE: Persistence via zustand/middleware is intentionally omitted here.
// zustand v5's middleware package uses import.meta.env which Metro's web
// bundler does not support. Once Expo/Metro adds import.meta support (or
// zustand ships a CJS-compatible build), re-add:
//   import { persist, createJSONStorage } from 'zustand/middleware'
// with platform-specific storage (localStorage on web, AsyncStorage on native).
export const useWardrobeStore = create<WardrobeState>((set, get) => ({
  detectionSteps: [],
  currentStepIndex: 0,
  items: {},
  // Default to 'onboarding' so any caller that doesn't specify keeps the
  // historical (full onboarding tail) behavior.
  flowOrigin: 'onboarding',

  initializeFlow: (steps, origin = 'onboarding') =>
    set({
      detectionSteps: steps,
      currentStepIndex: 0,
      items: {},
      flowOrigin: origin,
    }),

  setItemForStep: (stepIndex, item) =>
    set(state => ({
      items: { ...state.items, [stepIndex]: item },
    })),

  setTraitValue: (traitId, value) =>
    set(state => {
      const currentItem = state.items[state.currentStepIndex];
      if (!currentItem) return state;
      return {
        items: {
          ...state.items,
          [state.currentStepIndex]: {
            ...currentItem,
            traits: currentItem.traits.map(trait =>
              trait.id === traitId ? { ...trait, selectedValue: value } : trait
            ),
          },
        },
      };
    }),

  nextStep: () =>
    set(state => ({
      currentStepIndex: state.currentStepIndex + 1,
    })),

  previousStep: () =>
    set(state => ({
      currentStepIndex: Math.max(0, state.currentStepIndex - 1),
    })),

  getCurrentItem: () => {
    const state = get();
    return state.items[state.currentStepIndex] ?? null;
  },

  getItemForStep: stepIndex => get().items[stepIndex] ?? null,

  getTotalSteps: () => get().detectionSteps.length,

  getCurrentDetectionStep: () => {
    const state = get();
    return state.detectionSteps[state.currentStepIndex] ?? null;
  },

  isLastStep: () => {
    const state = get();
    return state.currentStepIndex >= state.detectionSteps.length - 1;
  },

  isFirstStep: () => get().currentStepIndex === 0,

  getAllItems: () => {
    const state = get();
    // Return all items in order, filtering out any null/undefined
    return Object.keys(state.items)
      .map(Number)
      .sort((a, b) => a - b)
      .map(index => state.items[index])
      .filter((item): item is WardrobeItem => item !== undefined);
  },

  reset: () =>
    set({
      detectionSteps: [],
      currentStepIndex: 0,
      items: {},
      flowOrigin: 'onboarding',
    }),
}));
