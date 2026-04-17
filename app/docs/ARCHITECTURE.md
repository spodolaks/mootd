# Mootd React Native - Architecture Reference

> This document serves as the development guide for the Mootd React Native application.
> All development should follow the patterns and conventions defined here.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Tech Stack](#tech-stack)
3. [Project Structure](#project-structure)
4. [Architecture Layers](#architecture-layers)
5. [Repository Pattern](#repository-pattern)
6. [State Management (Zustand)](#state-management-zustand)
7. [Component Standards](#component-standards)
8. [Design System](#design-system)
9. [Authentication](#authentication)
10. [Testing](#testing)
11. [Linting & Formatting](#linting--formatting)
12. [CI/CD Pipeline](#cicd-pipeline)
13. [Naming Conventions](#naming-conventions)
14. [Import Order](#import-order)
15. [Cleanup Checklist](#cleanup-checklist)

---

## Architecture Overview

This project uses a **Layered Architecture with Repository Pattern**, optimized for React Native. This architecture enables:

- **UI/Service decoupling**: Swap mock data for real API with zero UI changes
- **Testability**: Each layer can be tested independently
- **Scalability**: Feature-based modules grow independently
- **Maintainability**: Clear separation of concerns

```
┌─────────────────────────────────────────────────────────────────┐
│                      PRESENTATION LAYER                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Screens   │  │ Components  │  │   Hooks     │             │
│  │  (app/*.tsx)│  │  (src/ui/)  │  │ (useXxx)    │             │
│  └──────┬──────┘  └─────────────┘  └──────┬──────┘             │
│         │                                  │                    │
│         └──────────────┬───────────────────┘                    │
│                        ▼                                        │
├─────────────────────────────────────────────────────────────────┤
│                       DOMAIN LAYER                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │   Models    │  │  Interfaces │  │  Use Cases  │             │
│  │  (types)    │  │ (contracts) │  │ (optional)  │             │
│  └─────────────┘  └──────┬──────┘  └─────────────┘             │
│                          │                                      │
│                          ▼                                      │
├─────────────────────────────────────────────────────────────────┤
│                        DATA LAYER                               │
│  ┌─────────────────────────────────────────────────┐           │
│  │              REPOSITORIES                        │           │
│  │  ┌─────────────┐        ┌─────────────┐         │           │
│  │  │ MockSource  │   OR   │  APISource  │         │           │
│  │  │ (demo data) │        │ (real API)  │         │           │
│  │  └─────────────┘        └─────────────┘         │           │
│  └─────────────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

### Layer Rules

| Layer | Contains | Can Access | Cannot Access |
|-------|----------|------------|---------------|
| **Presentation** | Screens, Components, Hooks | Domain, Data (via repositories) | API directly |
| **Domain** | Models, Interfaces | Nothing (pure types) | Data, Presentation |
| **Data** | Repositories, Mock data, API client | Domain (for types) | Presentation |

---

## Tech Stack

| Category | Tool | Purpose |
|----------|------|---------|
| **Framework** | React Native + Expo | Cross-platform mobile |
| **Navigation** | Expo Router | File-based routing |
| **State** | Zustand | Global state management |
| **Styling** | StyleSheet + Theme tokens | Design system |
| **Icons** | Custom SVG icons | Consistent iconography |
| **Testing** | Jest + React Testing Library | Unit & integration tests |
| **Linting** | ESLint + Prettier | Code quality |
| **Type Check** | TypeScript (strict) | Type safety |
| **CI/CD** | GitHub Actions | Automated pipeline |

---

## Project Structure

```
mootd-react-native/
├── .github/
│   └── workflows/
│       └── ci.yml                    # GitHub Actions CI
│
├── app/                              # Expo Router (ROUTES ONLY)
│   ├── _layout.tsx                   # Root layout with providers
│   ├── (auth)/                       # Auth flow (unauthenticated)
│   │   ├── _layout.tsx
│   │   ├── login.tsx
│   │   └── signup.tsx
│   ├── (onboarding)/                 # Onboarding flow
│   │   └── _layout.tsx
│   └── (tabs)/                       # Main app (authenticated)
│       ├── _layout.tsx
│       ├── index.tsx
│       ├── explore.tsx
│       └── profile.tsx
│
├── src/
│   ├── components/
│   │   ├── ui/                       # Atomic UI components
│   │   │   ├── Button/
│   │   │   │   ├── Button.tsx
│   │   │   │   ├── styles.ts
│   │   │   │   ├── types.ts
│   │   │   │   ├── index.ts
│   │   │   │   └── __tests__/
│   │   │   │       └── Button.test.tsx
│   │   │   ├── Text/
│   │   │   ├── Input/
│   │   │   ├── Toggle/
│   │   │   ├── Modal/
│   │   │   └── ... (30+ components)
│   │   ├── layout/                   # Layout components
│   │   │   └── Screen/
│   │   └── icons/                    # Custom SVG icon system
│   │       └── Icon.tsx
│   │
│   ├── features/                     # Feature modules
│   │   ├── auth/
│   │   │   ├── components/           # Feature-specific components
│   │   │   ├── hooks/                # Feature hooks
│   │   │   │   └── useAuth.ts
│   │   │   └── screens/              # Screen content
│   │   │       ├── LoginScreen.tsx
│   │   │       └── SignupScreen.tsx
│   │   ├── mood/
│   │   │   ├── components/
│   │   │   ├── hooks/
│   │   │   └── screens/
│   │   └── onboarding/
│   │
│   ├── domain/                       # Domain layer (types only)
│   │   ├── models/                   # Data models/entities
│   │   │   ├── User.ts
│   │   │   ├── Mood.ts
│   │   │   └── index.ts
│   │   └── interfaces/               # Repository contracts
│   │       ├── IAuthRepository.ts
│   │       ├── IMoodRepository.ts
│   │       └── index.ts
│   │
│   ├── data/                         # Data layer
│   │   ├── repositories/             # Repository implementations
│   │   │   ├── auth/
│   │   │   │   ├── AuthRepository.mock.ts
│   │   │   │   └── AuthRepository.firebase.ts  (future)
│   │   │   ├── mood/
│   │   │   │   └── MoodRepository.mock.ts
│   │   │   └── index.ts              # SWAP POINT - exports active repos
│   │   ├── mock/                     # Demo/mock data
│   │   │   ├── users.ts
│   │   │   └── moods.ts
│   │   └── api/                      # API client (future)
│   │       └── client.ts
│   │
│   ├── store/                        # Zustand stores
│   │   ├── authStore.ts
│   │   ├── themeStore.ts
│   │   ├── uiStore.ts
│   │   └── index.ts
│   │
│   ├── hooks/                        # Shared hooks
│   │   ├── useColorScheme.ts
│   │   └── index.ts
│   │
│   ├── theme/                        # Design system tokens
│   │   ├── colors.ts
│   │   ├── typography.ts
│   │   ├── spacing.ts
│   │   ├── radius.ts
│   │   └── index.ts
│   │
│   ├── providers/                    # React Context providers
│   │   └── index.tsx
│   │
│   └── utils/                        # Utility functions
│       └── index.ts
│
├── assets/                           # Images, fonts
├── __mocks__/                        # Jest mocks
│
├── .eslintrc.js
├── .prettierrc
├── jest.config.js
├── tsconfig.json
├── app.json
└── package.json
```

---

## Architecture Layers

### Presentation Layer

**Location**: `app/`, `src/components/`, `src/features/*/screens/`, `src/features/*/hooks/`

- **Screens**: Route components in `app/` that render feature screens
- **Components**: Reusable UI elements (atomic design)
- **Hooks**: Data-fetching and state hooks that consume repositories

```typescript
// Screen in app/ - thin wrapper
// app/(tabs)/mood.tsx
import { MoodListScreen } from '@/src/features/mood/screens/MoodListScreen';

export default function MoodTab() {
  return <MoodListScreen />;
}
```

### Domain Layer

**Location**: `src/domain/`

- **Models**: TypeScript interfaces for data entities
- **Interfaces**: Repository contracts (abstractions)

```typescript
// src/domain/models/Mood.ts
export interface Mood {
  id: string;
  emoji: string;
  label: string;
  intensity: number;
  createdAt: Date;
  note?: string;
}

export interface CreateMoodDTO {
  emoji: string;
  label: string;
  intensity: number;
  note?: string;
}
```

### Data Layer

**Location**: `src/data/`

- **Repositories**: Implementations of domain interfaces
- **Mock Data**: Static demo data
- **API Client**: HTTP client for real backend (future)

---

## Repository Pattern

### How It Works

```
UI Component
    │
    ▼
Feature Hook (useMoods)
    │
    ▼
Repository Interface (IMoodRepository)
    │
    ├──► MockMoodRepository (NOW)
    │
    └──► APIMoodRepository (FUTURE)
```

### Implementation

#### 1. Define Interface (Contract)

```typescript
// src/domain/interfaces/IMoodRepository.ts
import type { Mood, CreateMoodDTO } from '../models/Mood';

export interface IMoodRepository {
  getAll(): Promise<Mood[]>;
  getById(id: string): Promise<Mood | null>;
  create(data: CreateMoodDTO): Promise<Mood>;
  delete(id: string): Promise<void>;
}
```

#### 2. Create Mock Implementation

```typescript
// src/data/repositories/mood/MoodRepository.mock.ts
import type { IMoodRepository } from '@/src/domain/interfaces';
import type { Mood, CreateMoodDTO } from '@/src/domain/models';
import { MOCK_MOODS } from '@/src/data/mock/moods';

export class MockMoodRepository implements IMoodRepository {
  private moods: Mood[] = [...MOCK_MOODS];

  async getAll(): Promise<Mood[]> {
    await this.delay(300);
    return this.moods;
  }

  async getById(id: string): Promise<Mood | null> {
    await this.delay(200);
    return this.moods.find(m => m.id === id) ?? null;
  }

  async create(data: CreateMoodDTO): Promise<Mood> {
    await this.delay(400);
    const newMood: Mood = {
      id: Date.now().toString(),
      ...data,
      createdAt: new Date(),
    };
    this.moods.unshift(newMood);
    return newMood;
  }

  async delete(id: string): Promise<void> {
    await this.delay(200);
    this.moods = this.moods.filter(m => m.id !== id);
  }

  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}
```

#### 3. Export Active Implementation (Swap Point)

```typescript
// src/data/repositories/index.ts
import { MockMoodRepository } from './mood/MoodRepository.mock';
import { MockAuthRepository } from './auth/AuthRepository.mock';
// import { APIMoodRepository } from './mood/MoodRepository.api';

import type { IMoodRepository, IAuthRepository } from '@/src/domain/interfaces';

// ============================================
// SWAP POINT: Change implementation here
// ============================================
export const moodRepository: IMoodRepository = new MockMoodRepository();
export const authRepository: IAuthRepository = new MockAuthRepository();

// When ready for real API:
// export const moodRepository: IMoodRepository = new APIMoodRepository();
```

#### 4. Consume in Feature Hook

```typescript
// src/features/mood/hooks/useMoods.ts
import { useState, useEffect, useCallback } from 'react';
import { moodRepository } from '@/src/data/repositories';
import type { Mood, CreateMoodDTO } from '@/src/domain/models';

interface UseMoodsResult {
  moods: Mood[];
  isLoading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  addMood: (data: CreateMoodDTO) => Promise<void>;
  deleteMood: (id: string) => Promise<void>;
}

export function useMoods(): UseMoodsResult {
  const [moods, setMoods] = useState<Mood[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchMoods = useCallback(async () => {
    try {
      setIsLoading(true);
      setError(null);
      const data = await moodRepository.getAll();
      setMoods(data);
    } catch (e) {
      setError('Failed to load moods');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchMoods();
  }, [fetchMoods]);

  const addMood = async (data: CreateMoodDTO) => {
    const newMood = await moodRepository.create(data);
    setMoods(prev => [newMood, ...prev]);
  };

  const deleteMood = async (id: string) => {
    await moodRepository.delete(id);
    setMoods(prev => prev.filter(m => m.id !== id));
  };

  return { moods, isLoading, error, refresh: fetchMoods, addMood, deleteMood };
}
```

#### 5. Use in Screen (Knows Nothing About Data Source)

```typescript
// src/features/mood/screens/MoodListScreen.tsx
import { View, FlatList } from 'react-native';
import { Text, Spinner, Button } from '@/src/components/ui';
import { MoodCard } from '../components/MoodCard';
import { useMoods } from '../hooks/useMoods';

export function MoodListScreen() {
  const { moods, isLoading, error, refresh } = useMoods();

  if (isLoading) {
    return <Spinner />;
  }

  if (error) {
    return (
      <View>
        <Text>{error}</Text>
        <Button label="Retry" onPress={refresh} />
      </View>
    );
  }

  return (
    <FlatList
      data={moods}
      keyExtractor={item => item.id}
      renderItem={({ item }) => <MoodCard mood={item} />}
      onRefresh={refresh}
      refreshing={isLoading}
    />
  );
}
```

---

## State Management (Zustand)

### Store Structure

```
┌─────────────────────────────────────────────────┐
│                    App State                     │
├─────────────────────────────────────────────────┤
│  AuthStore     │  ThemeStore   │  UIStore       │
│  - user        │  - preference │  - toasts      │
│  - isLoading   │               │  - modals      │
│  - isAuth      │               │                │
└─────────────────────────────────────────────────┘
```

### Auth Store Example

```typescript
// src/store/authStore.ts
import { create } from 'zustand';
import { authRepository } from '@/src/data/repositories';
import type { User } from '@/src/domain/models';

interface AuthState {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;

  // Actions
  signInWithGoogle: () => Promise<void>;
  signInWithApple: () => Promise<void>;
  signOut: () => Promise<void>;
  checkAuth: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isAuthenticated: false,

  signInWithGoogle: async () => {
    set({ isLoading: true });
    try {
      const user = await authRepository.signInWithGoogle();
      set({ user, isAuthenticated: true });
    } finally {
      set({ isLoading: false });
    }
  },

  signInWithApple: async () => {
    set({ isLoading: true });
    try {
      const user = await authRepository.signInWithApple();
      set({ user, isAuthenticated: true });
    } finally {
      set({ isLoading: false });
    }
  },

  signOut: async () => {
    await authRepository.signOut();
    set({ user: null, isAuthenticated: false });
  },

  checkAuth: async () => {
    set({ isLoading: true });
    const user = await authRepository.getCurrentUser();
    set({ user, isAuthenticated: !!user, isLoading: false });
  },
}));
```

### Theme Store with Persistence

```typescript
// src/store/themeStore.ts
import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import AsyncStorage from '@react-native-async-storage/async-storage';

type ThemePreference = 'light' | 'dark' | 'system';

interface ThemeState {
  preference: ThemePreference;
  setPreference: (pref: ThemePreference) => void;
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set) => ({
      preference: 'system',
      setPreference: (preference) => set({ preference }),
    }),
    {
      name: 'theme-storage',
      storage: createJSONStorage(() => AsyncStorage),
    }
  )
);
```

### UI Store (Toasts, Modals)

```typescript
// src/store/uiStore.ts
import { create } from 'zustand';

interface Toast {
  id: string;
  message: string;
  type: 'success' | 'error' | 'info';
}

interface UIState {
  toasts: Toast[];
  showToast: (message: string, type?: Toast['type']) => void;
  dismissToast: (id: string) => void;
}

export const useUIStore = create<UIState>((set) => ({
  toasts: [],

  showToast: (message, type = 'info') => {
    const id = Date.now().toString();
    set((state) => ({
      toasts: [...state.toasts, { id, message, type }],
    }));
    // Auto-dismiss after 3 seconds
    setTimeout(() => {
      set((state) => ({
        toasts: state.toasts.filter((t) => t.id !== id),
      }));
    }, 3000);
  },

  dismissToast: (id) => {
    set((state) => ({
      toasts: state.toasts.filter((t) => t.id !== id),
    }));
  },
}));
```

---

## Component Standards

### Atomic Design Hierarchy

```
Atoms      → Button, Text, Input, Icon, Toggle, Spinner
Molecules  → Chip, ListItem, SelectableItem, TabBar
Organisms  → Modal, List, WeatherCard, Toast
Templates  → Screen layouts
Pages      → app/ routes (Expo Router)
```

### Component File Structure

```
ComponentName/
├── ComponentName.tsx      # Main component
├── styles.ts             # Style definitions
├── types.ts              # TypeScript interfaces
├── index.ts              # Exports
└── __tests__/
    └── ComponentName.test.tsx
```

### Standard Component Pattern

```typescript
// ComponentName/types.ts
import type { StyleProp, ViewStyle } from 'react-native';

export interface ComponentNameProps {
  /** Required prop description */
  label: string;
  /** Optional variant */
  variant?: 'primary' | 'secondary';
  /** Event handler */
  onPress?: () => void;
  /** Style overrides (always last) */
  style?: StyleProp<ViewStyle>;
}

// ComponentName/styles.ts
import { StyleSheet } from 'react-native';
import type { ColorMode } from '@/src/theme';
import { colors, spacing, radius } from '@/src/theme';

export const getStyles = (mode: ColorMode) =>
  StyleSheet.create({
    container: {
      padding: spacing.md,
      borderRadius: radius.md,
      backgroundColor: colors.backgrounds.primary[mode],
    },
    primary: {
      backgroundColor: colors.button.primary.background[mode],
    },
    secondary: {
      backgroundColor: colors.button.secondary.background[mode],
    },
  });

// ComponentName/ComponentName.tsx
import { View } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { getStyles } from './styles';
import type { ComponentNameProps } from './types';

export function ComponentName({
  label,
  variant = 'primary',
  style,
  onPress,
}: ComponentNameProps) {
  const colorScheme = useColorScheme();
  const styles = getStyles(colorScheme);

  return (
    <View style={[styles.container, styles[variant], style]}>
      {/* Component content */}
    </View>
  );
}

// ComponentName/index.ts
export { ComponentName } from './ComponentName';
export type { ComponentNameProps } from './types';
```

---

## Design System

### Token Usage Rules

| Token | Usage | Example |
|-------|-------|---------|
| **Colors** | ALWAYS use theme tokens | `colors.backgrounds.primary[mode]` |
| **Typography** | Use typography scale | `typography.body.regular` |
| **Spacing** | Use spacing scale | `spacing.md` (16px) |
| **Radius** | Use radius scale | `radius.lg` (12px) |
| **Icons** | Use custom Icon component | `<Icon name="plus" />` |

### Color System

```typescript
// NEVER do this:
backgroundColor: '#FF0000'

// ALWAYS do this:
backgroundColor: colors.accents.red[mode]
```

### Spacing Scale

| Token | Value | Usage |
|-------|-------|-------|
| `spacing.xs` | 4px | Tight spacing |
| `spacing.sm` | 8px | Small gaps |
| `spacing.md` | 16px | Default padding |
| `spacing.lg` | 24px | Section spacing |
| `spacing.xl` | 32px | Large gaps |
| `spacing.xxl` | 48px | Page margins |

### Border Radius

| Token | Value | Usage |
|-------|-------|-------|
| `radius.none` | 0 | Sharp corners |
| `radius.sm` | 4px | Subtle rounding |
| `radius.md` | 8px | Default rounding |
| `radius.lg` | 12px | Cards, modals |
| `radius.xl` | 16px | Large elements |
| `radius.full` | 9999px | Pills, circles |

---

## Authentication

### Auth Repository Interface

```typescript
// src/domain/interfaces/IAuthRepository.ts
import type { User } from '../models/User';

export interface IAuthRepository {
  signInWithGoogle(): Promise<User>;
  signInWithApple(): Promise<User>;
  signInWithEmail(email: string, password: string): Promise<User>;
  signUp(email: string, password: string, name: string): Promise<User>;
  signOut(): Promise<void>;
  getCurrentUser(): Promise<User | null>;
  onAuthStateChange(callback: (user: User | null) => void): () => void;
}
```

### Mock Auth Repository

```typescript
// src/data/repositories/auth/AuthRepository.mock.ts
import type { IAuthRepository } from '@/src/domain/interfaces';
import type { User } from '@/src/domain/models';
import { MOCK_USER } from '@/src/data/mock/users';

export class MockAuthRepository implements IAuthRepository {
  private currentUser: User | null = null;
  private listeners: ((user: User | null) => void)[] = [];

  async signInWithGoogle(): Promise<User> {
    await this.delay(800);
    this.currentUser = MOCK_USER;
    this.notifyListeners();
    return this.currentUser;
  }

  async signInWithApple(): Promise<User> {
    await this.delay(800);
    this.currentUser = MOCK_USER;
    this.notifyListeners();
    return this.currentUser;
  }

  async signInWithEmail(email: string, password: string): Promise<User> {
    await this.delay(600);
    // Simulate validation
    if (!email || !password) {
      throw new Error('Invalid credentials');
    }
    this.currentUser = { ...MOCK_USER, email };
    this.notifyListeners();
    return this.currentUser;
  }

  async signUp(email: string, password: string, name: string): Promise<User> {
    await this.delay(800);
    this.currentUser = { ...MOCK_USER, email, name };
    this.notifyListeners();
    return this.currentUser;
  }

  async signOut(): Promise<void> {
    await this.delay(300);
    this.currentUser = null;
    this.notifyListeners();
  }

  async getCurrentUser(): Promise<User | null> {
    await this.delay(200);
    return this.currentUser;
  }

  onAuthStateChange(callback: (user: User | null) => void): () => void {
    this.listeners.push(callback);
    return () => {
      this.listeners = this.listeners.filter(l => l !== callback);
    };
  }

  private notifyListeners(): void {
    this.listeners.forEach(l => l(this.currentUser));
  }

  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}
```

---

## Testing

### Test File Location

Tests live alongside the code they test:

```
src/components/ui/Button/
├── Button.tsx
├── __tests__/
│   └── Button.test.tsx
```

### Component Test Example

```typescript
// src/components/ui/Button/__tests__/Button.test.tsx
import { render, fireEvent } from '@testing-library/react-native';
import { Button } from '../Button';

describe('Button', () => {
  it('renders label correctly', () => {
    const { getByText } = render(<Button label="Click me" />);
    expect(getByText('Click me')).toBeTruthy();
  });

  it('calls onPress when pressed', () => {
    const onPress = jest.fn();
    const { getByText } = render(<Button label="Click" onPress={onPress} />);
    fireEvent.press(getByText('Click'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('does not call onPress when disabled', () => {
    const onPress = jest.fn();
    const { getByText } = render(
      <Button label="Click" onPress={onPress} disabled />
    );
    fireEvent.press(getByText('Click'));
    expect(onPress).not.toHaveBeenCalled();
  });

  it('renders with different variants', () => {
    const { rerender, getByTestId } = render(
      <Button label="Test" variant="primary" testID="button" />
    );
    expect(getByTestId('button')).toBeTruthy();

    rerender(<Button label="Test" variant="secondary" testID="button" />);
    expect(getByTestId('button')).toBeTruthy();
  });
});
```

### Hook Test Example

```typescript
// src/features/mood/hooks/__tests__/useMoods.test.ts
import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useMoods } from '../useMoods';

jest.mock('@/src/data/repositories', () => ({
  moodRepository: {
    getAll: jest.fn().mockResolvedValue([
      { id: '1', emoji: '😊', label: 'Happy', intensity: 4 },
    ]),
    create: jest.fn().mockImplementation((data) =>
      Promise.resolve({ id: '2', ...data, createdAt: new Date() })
    ),
    delete: jest.fn().mockResolvedValue(undefined),
  },
}));

describe('useMoods', () => {
  it('fetches moods on mount', async () => {
    const { result } = renderHook(() => useMoods());

    expect(result.current.isLoading).toBe(true);

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(result.current.moods).toHaveLength(1);
    expect(result.current.moods[0].label).toBe('Happy');
  });

  it('adds a new mood', async () => {
    const { result } = renderHook(() => useMoods());

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    await act(async () => {
      await result.current.addMood({
        emoji: '😢',
        label: 'Sad',
        intensity: 2,
      });
    });

    expect(result.current.moods).toHaveLength(2);
  });
});
```

### Repository Test Example

```typescript
// src/data/repositories/mood/__tests__/MoodRepository.test.ts
import { MockMoodRepository } from '../MoodRepository.mock';

describe('MockMoodRepository', () => {
  let repository: MockMoodRepository;

  beforeEach(() => {
    repository = new MockMoodRepository();
  });

  it('returns all moods', async () => {
    const moods = await repository.getAll();
    expect(Array.isArray(moods)).toBe(true);
  });

  it('creates a new mood', async () => {
    const newMood = await repository.create({
      emoji: '🎉',
      label: 'Excited',
      intensity: 5,
    });

    expect(newMood.id).toBeDefined();
    expect(newMood.label).toBe('Excited');
    expect(newMood.createdAt).toBeInstanceOf(Date);
  });

  it('deletes a mood', async () => {
    const moods = await repository.getAll();
    const initialCount = moods.length;

    await repository.delete(moods[0].id);

    const updatedMoods = await repository.getAll();
    expect(updatedMoods.length).toBe(initialCount - 1);
  });
});
```

---

## Linting & Formatting

### ESLint Configuration

```javascript
// .eslintrc.js
module.exports = {
  root: true,
  extends: [
    'expo',
    '@react-native',
    'plugin:@typescript-eslint/recommended',
    'prettier',
  ],
  plugins: ['@typescript-eslint', 'import'],
  parser: '@typescript-eslint/parser',
  rules: {
    'import/order': [
      'error',
      {
        groups: [
          'builtin',
          'external',
          'internal',
          ['parent', 'sibling'],
          'index',
        ],
        pathGroups: [
          { pattern: 'react', group: 'builtin', position: 'before' },
          { pattern: 'react-native', group: 'builtin', position: 'before' },
          { pattern: '@/**', group: 'internal', position: 'before' },
        ],
        pathGroupsExcludedImportTypes: ['react', 'react-native'],
        'newlines-between': 'always',
        alphabetize: { order: 'asc', caseInsensitive: true },
      },
    ],
    '@typescript-eslint/explicit-function-return-type': 'off',
    '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    'react/react-in-jsx-scope': 'off',
    'react-hooks/exhaustive-deps': 'warn',
  },
};
```

### Prettier Configuration

```json
// .prettierrc
{
  "semi": true,
  "singleQuote": true,
  "tabWidth": 2,
  "trailingComma": "es5",
  "printWidth": 100,
  "bracketSpacing": true,
  "arrowParens": "avoid"
}
```

### Package Scripts

```json
{
  "scripts": {
    "start": "expo start",
    "android": "expo start --android",
    "ios": "expo start --ios",
    "web": "expo start --web",

    "lint": "eslint . --ext .ts,.tsx",
    "lint:fix": "eslint . --ext .ts,.tsx --fix",
    "format": "prettier --write \"src/**/*.{ts,tsx}\"",
    "format:check": "prettier --check \"src/**/*.{ts,tsx}\"",

    "typecheck": "tsc --noEmit",

    "test": "jest",
    "test:watch": "jest --watch",
    "test:coverage": "jest --coverage",

    "validate": "npm run typecheck && npm run lint && npm run test",

    "build:android": "eas build --platform android",
    "build:ios": "eas build --platform ios"
  }
}
```

---

## CI/CD Pipeline

### GitHub Actions

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

jobs:
  validate:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'

      - name: Install dependencies
        run: npm ci

      - name: TypeScript check
        run: npm run typecheck

      - name: Lint
        run: npm run lint

      - name: Format check
        run: npm run format:check

      - name: Test
        run: npm run test -- --coverage

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage/lcov.info
```

---

## Naming Conventions

| Type | Convention | Example |
|------|------------|---------|
| **Components** | PascalCase | `GradientButton.tsx` |
| **Hooks** | camelCase with `use` prefix | `useColorScheme.ts` |
| **Stores** | camelCase with `Store` suffix | `authStore.ts` |
| **Repositories** | PascalCase with `Repository` suffix | `MoodRepository.mock.ts` |
| **Interfaces** | PascalCase with `I` prefix | `IMoodRepository.ts` |
| **Types** | PascalCase + `Props`/`State`/`DTO` | `ButtonProps`, `CreateMoodDTO` |
| **Utils** | camelCase | `formatDate.ts` |
| **Constants** | UPPER_SNAKE_CASE | `API_BASE_URL` |
| **Theme tokens** | camelCase | `spacing.md` |
| **Mock data files** | camelCase plural | `moods.ts`, `users.ts` |

---

## Import Order

```typescript
// 1. React/React Native
import React, { useState, useEffect } from 'react';
import { View, StyleSheet } from 'react-native';

// 2. External libraries
import { useRouter } from 'expo-router';
import Animated from 'react-native-reanimated';

// 3. Internal - absolute imports (stores, repositories, components)
import { useAuthStore } from '@/src/store';
import { Button, Text } from '@/src/components/ui';
import { useColorScheme } from '@/src/hooks';
import { colors, spacing } from '@/src/theme';

// 4. Internal - relative imports (local files)
import { getStyles } from './styles';
import type { ComponentProps } from './types';
```

---

## Cleanup Checklist

### Phase 1: Consolidation (Immediate)

- [ ] Delete `constants/theme.ts` (legacy Expo starter)
- [ ] Delete `components/` folder (legacy Expo starter)
- [ ] Move `hooks/` → `src/hooks/`
- [ ] Install Zustand + AsyncStorage
- [ ] Install testing dependencies
- [ ] Setup ESLint + Prettier configs
- [ ] Create GitHub Actions CI workflow

### Phase 2: Standardization

- [ ] Fix Modal.tsx to use custom `useColorScheme` hook
- [ ] Fix GradientButton.tsx to use theme tokens (no hardcoded colors)
- [ ] Audit all components for pattern compliance
- [ ] Add `types.ts` to components missing it
- [ ] Replace `@expo/vector-icons` with custom Icon in tab navigation

### Phase 3: Infrastructure

- [ ] Create `src/domain/` folder structure
- [ ] Create `src/data/` folder structure
- [ ] Create `src/store/` with Zustand stores
- [ ] Create `src/features/` folder structure
- [ ] Add Error Boundary at root layout
- [ ] Create Screen wrapper component

### Phase 4: Documentation & Testing

- [ ] Add tests for existing UI components
- [ ] Add tests for hooks
- [ ] Add component usage examples

---

## Dependencies to Add

```bash
# State management
npm install zustand @react-native-async-storage/async-storage

# Testing
npm install -D jest @testing-library/react-native @testing-library/jest-native jest-expo

# Linting
npm install -D eslint prettier eslint-config-prettier eslint-plugin-import @typescript-eslint/eslint-plugin @typescript-eslint/parser
```

---

## Quick Reference

### Creating a New Feature

1. Create folder: `src/features/{feature-name}/`
2. Add subfolders: `components/`, `hooks/`, `screens/`
3. Create domain model: `src/domain/models/{Model}.ts`
4. Create repository interface: `src/domain/interfaces/I{Model}Repository.ts`
5. Create mock repository: `src/data/repositories/{feature}/`
6. Export repository in `src/data/repositories/index.ts`
7. Create feature hook that consumes repository
8. Create screen that uses hook
9. Add route in `app/`

### Swapping to Real API

1. Create `{Repository}.api.ts` implementing the interface
2. Change export in `src/data/repositories/index.ts`
3. Done - no UI changes needed

---

*Last updated: December 2024*
