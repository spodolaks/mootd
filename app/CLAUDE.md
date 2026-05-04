# MOOTD App — Architecture & Conventions

React Native + Expo app (SDK 54, RN 0.83, TypeScript strict).

## Directory Layout

```
app/
├── app/                    # Expo Router file-based routes
│   ├── _layout.tsx         # Root: fonts, error boundary, auth restore, session gate
│   ├── index.tsx           # Welcome/login screen
│   ├── (main)/             # Protected tab navigator (moodboard, wardrobe, calendar, profile)
│   ├── build-wardrobe.tsx  # Wardrobe onboarding
│   ├── detected-item.tsx   # Post-detection review
│   ├── trait-selection.tsx # Item attribute picker
│   └── item-details.tsx    # Item detail + edit
├── src/
│   ├── store/              # Zustand stores (auth, preferences, wardrobe, ui)
│   ├── screens/            # Screen components grouped by domain
│   ├── components/
│   │   ├── ui/             # 20+ shared UI components
│   │   └── moodboard/      # Moodboard-specific components
│   │       ├── Collage.tsx         # Outfit photo collage layout
│   │       ├── OutfitCard.tsx      # Single outfit display card
│   │       ├── SavedBoardView.tsx  # Saved moodboard gallery
│   │       └── ArchetypeBadges.tsx # Style archetype tag display
│   ├── hooks/              # Custom hooks
│   ├── data/
│   │   ├── api/client.ts   # HTTP client with JWT, token refresh, timeout, error handling
│   │   └── repositories/   # API + Mock implementations per interface
│   ├── domain/
│   │   ├── interfaces/     # Repository interfaces (IAuthRepository, IWardrobeRepository, IOutfitRepository)
│   │   └── models/         # Domain types (AuthUser, WardrobeItem, Outfit, etc.)
│   └── theme/              # colors.ts, typography.ts, spacing.ts, radius.ts
```

Use `@/` for absolute imports: `import { wardrobeRepository } from '@/src/data/repositories'`.

## State Management

Four Zustand stores — one per concern:

| Store | Contents |
|-------|----------|
| `authStore` | `user`, `session`, `isAuthenticated`, `sessionRestored`, auth actions |
| `preferencesStore` | theme, notifications, temperatureUnit, displayName |
| `wardrobeStore` | multi-step detection wizard state (not persisted) |
| `uiStore` | toasts, global loading state |

**Critical**: `sessionRestored` in `authStore` signals that `restoreSession()` completed. The root layout blocks rendering until `sessionRestored === true` — this prevents race conditions where components fire API calls before the JWT is in memory.

**Persistence**: No `zustand/middleware` — Metro bundler incompatibility. Each store handles its own persistence manually:
- Tokens (access + refresh) → `SecureStore` (native) / `localStorage` (web)
- Preferences → `AsyncStorage` (native) / `localStorage` (web)

## Data Layer

### Repository Pattern

Every data operation goes through a repository interface:
1. Define in `src/domain/interfaces/I<Name>Repository.ts`
2. Implement as `src/data/repositories/<name>/<Name>Repository.api.ts`
3. Add mock as `src/data/repositories/<name>/<Name>Repository.mock.ts`
4. Export active instance from `src/data/repositories/index.ts` based on `EXPO_PUBLIC_DATA_SOURCE`

```typescript
// Active instance selection
const activeDataSource = process.env.EXPO_PUBLIC_DATA_SOURCE ?? 'mock';
export const wardrobeRepository: IWardrobeRepository =
  activeDataSource === 'api' ? new ApiWardrobeRepository() : new MockWardrobeRepository();
```

### API Client

`src/data/api/client.ts` — all HTTP calls go through `apiClient`:
- Auto-includes `Authorization: Bearer <token>` when set via `setAuthToken()`
- **401 interceptor**: On receiving a 401, the client automatically attempts to refresh the access token using the stored refresh token. If the refresh succeeds, the original request is retried transparently. If the refresh fails, the user is logged out.
- Default timeout: 10s (use `init` override for long operations)
- Throws `ApiError` with `status` and `details` on non-2xx

### Async Outfit Generation

Outfit generation uses a polling pattern:
1. `POST /v1/outfits/generate` — returns `{ jobId }` immediately
2. Client polls `GET /v1/outfits/jobs/{jobId}` on an interval until status is `completed` or `failed`
3. UI shows a loading/progress state during polling

### Cursor Pagination (Infinite Scroll)

The `WardrobeScreen` uses cursor-based pagination for loading wardrobe items:
- Initial load fetches the first page
- Scrolling to the bottom triggers loading the next page using the `nextCursor` from the previous response
- `FlatList` with `onEndReached` handles the infinite scroll trigger
- Loading indicator shown at the list footer while fetching

### Environment Variables

```bash
EXPO_PUBLIC_DATA_SOURCE=api   # 'mock' for offline dev, 'api' for real backend
EXPO_PUBLIC_API_URL=http://127.0.0.1:8081
```

## Navigation

File-based routing via Expo Router. Key routes:
- Unauthenticated: `index.tsx` (login)
- Authenticated: `(main)/` tab group (moodboard, wardrobe, calendar, profile)
- Auth gate: root `_layout.tsx` renders `null` until `fontsLoaded && sessionRestored`

After login, check wardrobe count to decide where to send user:
- Empty wardrobe → `/build-wardrobe`
- Has items → `/(main)/moodboard`

## Styling Conventions

Always use design system constants — no magic numbers:

```typescript
import { backgrounds, labels, fills, grays, button } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { spacing } from '@/src/theme/spacing';  // xs:4, sm:8, md:16, lg:24, xl:32
import { radius } from '@/src/theme/radius';    // sm:4, md:8, lg:12, xl:16, full:9999

const colorScheme = useColorScheme() ?? 'light';
const textColor = labels.primary[colorScheme];
const bgColor = backgrounds.primary[colorScheme];
```

Typography objects include font family, size, line height, and letter spacing:
```typescript
style={[styles.title, { color: textColor }]}
// In StyleSheet:
title: { ...typography.largeTitle.semiBold }
```

## Component Conventions

- Functional components typed as `React.FC<Props>` or inline function with explicit return type
- Color-aware: always read colors from `useColorScheme()` — never hardcode light/dark colors
- Use `SafeAreaView` from `react-native-safe-area-context` for screens
- For images, use `expo-image` with `cachePolicy` for optimized loading and caching. For images that may have expired URLs (signed GCS links), track `onError` state per image and fall back to a placeholder icon

### testID + accessibilityLabel (mootd#52)

Every interactive surface must carry both:

- **`testID`** — kebab-case, action-verb-prefixed. Stable selector for future Maestro / Detox flows. Examples: `login-google`, `tab-moodboard`, `wardrobe-item-{id}`, `moodboard-generate`, `outfit-card-choose`.
- **`accessibilityLabel`** — full sentence read by VoiceOver / TalkBack when no visible text describes the control. Icon-only buttons (heart, eye, swap) MUST set this; text buttons may omit it (the label is read from the visible label).

Conventions:
- Lists carry per-item testIDs `{collection}-item-{id}` so a test can assert on a specific row.
- Tab bar uses `tab-{routeName}` so the tab key matches the file-route name.
- Modals + popovers tag their primary action with the verb (`save-confirm`, `delete-confirm`).

## Adding a New Screen

1. Create route file: `app/my-screen.tsx`
2. Wrap in `SafeAreaView` with `backgroundColor` from theme
3. Use `useColorScheme()` for all colors
4. Navigate with `useRouter()`: `router.push('/my-screen')` or `router.back()`
5. For route params: `useLocalSearchParams<{ id: string; name: string }>()`

## Adding a New Wardrobe/API Feature

1. Add method to `IWardrobeRepository` in `src/domain/interfaces/`
2. Implement in `WardrobeRepository.api.ts` using `apiClient`
3. Add stub (delay + return) in `WardrobeRepository.mock.ts`
4. Update `backend/internal/wardrobe/` with matching endpoint (see backend/CLAUDE.md)
5. Keep TypeScript domain types in `src/domain/models/` in sync with Go structs

## Common Patterns

**Async button handler** (avoid floating promises):
```typescript
<GradientButton label="Save" onPress={() => { void handleSave(); }} />
```

**Loading + error in a screen**:
```typescript
const [isSaving, setIsSaving] = useState(false);
const handleSave = async () => {
  setIsSaving(true);
  try {
    await repository.doSomething();
    router.back();
  } catch (e) {
    Alert.alert('Error', e instanceof Error ? e.message : 'Something went wrong.');
  } finally {
    setIsSaving(false);
  }
};
```

**Image with expo-image and fallback** (for signed URLs that expire):
```typescript
import { Image } from 'expo-image';

const [imgError, setImgError] = useState(false);
{imageUrl && !imgError ? (
  <Image source={{ uri: imageUrl }} cachePolicy="disk" onError={() => setImgError(true)} />
) : (
  <Icon name="closet" size={32} color={placeholderColor} />
)}
```

## Running & Building

```bash
npm start          # Expo dev server
npm run ios        # iOS simulator
npm run android    # Android emulator
npm run web        # Browser
npm run validate   # typecheck + lint + test
```

Tests use `jest-expo` preset with 50% coverage threshold. No test files exist yet.
