import { accents, backgrounds, button, fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import type { Outfit, OutfitWeather, WardrobeItem } from '@/src/domain';
import React, { useImperativeHandle, useRef } from 'react';
import { ActivityIndicator, Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import ViewShot from 'react-native-view-shot';
import { Collage } from '@/src/components/moodboard/Collage';
import { ArchetypeBadges } from '@/src/components/moodboard/ArchetypeBadges';
import { Icon } from '@/src/components';
import { SCREEN_WIDTH, CONTAINER_PADDING, MAX_CARD_WIDTH } from '@/src/components/moodboard/constants';
import { conditionIcon } from '@/src/components/moodboard/weatherChip';
import { toPng } from '@/src/lib/htmlToImage';

/** Platform-agnostic capture handle exposed by OutfitCard. Parent calls
 *  capture() at Save time; the implementation swaps between
 *  react-native-view-shot (iOS/Android) and html-to-image (web) so the
 *  same call site works everywhere. Returns a full data URL
 *  ("data:image/png;base64,…") or null when capture isn't possible. */
export interface CollageCaptureHandle {
  capture: () => Promise<string | null>;
}

/** Secondary weather detail — rendered as a small caption under the chip
 *  row so the top of the screen doesn't need a dedicated weather card. */
export interface WeatherDetail {
  location: string;
  highTemperature: number;
  lowTemperature: number;
  unit: 'c' | 'f';
}

/**
 * Builds the tracking-caps eyebrow line combining the LOOK counter with
 * condensed weather context. Folding all meta into a single line is the
 * main noise-reduction trick — saves two rows vs. separate weather chip +
 * location/H-L caption, and reads as editorial rather than data-dense.
 *
 * Example: `LOOK 1 / 3  ·  NEW YORK  ·  19° ☁  H26 / L15`
 */
const buildEyebrow = (
  index: number,
  total: number,
  detail?: WeatherDetail,
  weather?: OutfitWeather,
): string => {
  const parts: string[] = [`LOOK ${index + 1} / ${total}`];
  if (detail?.location) parts.push(detail.location.toUpperCase());

  const temp = weather?.temperature ? `${weather.temperature}°` : '';
  const icon = conditionIcon(weather?.condition);
  const current = [temp, icon].filter(Boolean).join(' ');
  if (current) parts.push(current);

  if (detail) {
    const u = detail.unit.toUpperCase();
    parts.push(
      `H${Math.round(detail.highTemperature)}°${u} / L${Math.round(detail.lowTemperature)}°${u}`,
    );
  }
  return parts.join('   ·   ');
};

export interface OutfitCardProps {
  outfit: Outfit;
  index: number;
  total: number;
  itemMap: Map<string, WardrobeItem>;
  onSelect: () => void;
  onItemPress?: (itemId: string) => void;
  isSaving: boolean;
  colorScheme: 'light' | 'dark';
  cardHeight: number;
  /** Optional location + high/low temp, surfaced as a caption under the
   *  chip row so the top of the screen can drop its dedicated weather card. */
  weatherDetail?: WeatherDetail;
  /** Fires when the user taps thumbs-up on this outfit. No-op when absent —
   *  the whole rating row is hidden so there's no dead UI. */
  onThumbsUp?: () => void;
  /** Fires when the user taps thumbs-down on this outfit. */
  onThumbsDown?: () => void;
  /** Current rating the parent has recorded for this outfit, so the selected
   *  button can render in an active state. Immutable-per-card: once set the
   *  buttons lock (append-only event log, no undo). `null` means unrated. */
  rating?: 'up' | 'down' | null;
  /** Parent-supplied ref that receives a CollageCaptureHandle. Calling
   *  .capture() returns a full data URL (PNG) suitable for POST
   *  /v1/moodboards `boardImage`. The handle hides the native / web split:
   *  on iOS/Android it captures the ViewShot wrapping the collage, on web
   *  it uses html-to-image on the underlying DOM node. */
  collageCaptureRef?: React.Ref<CollageCaptureHandle>;
}

// F2 + F1 + F3: memoised base so OutfitCard only re-renders when its
// data-bearing props change. The FlatList parent recreates inline
// callbacks (onSelect / onItemPress / onThumbsUp / onThumbsDown /
// collageCaptureRef) every render, so default shallow memo would miss
// every time. propsAreEqual below explicitly ignores callback identity
// and relies on their semantic stability — handlers like
// setSwapTarget, handleRateOutfit, etc. always do the same thing for
// the same outfit/index pair, so fresh references are safe to ignore.
const OutfitCardBase: React.FC<OutfitCardProps> = ({
  outfit, index, total, itemMap, onSelect, onItemPress, isSaving, colorScheme, cardHeight, weatherDetail,
  onThumbsUp, onThumbsDown, rating, collageCaptureRef,
}) => {
  const viewShotRef = useRef<ViewShot | null>(null);
  const webNodeRef = useRef<View | null>(null);

  // Expose a unified capture() to the parent. Dynamic-dispatch over the two
  // refs keeps the render tree identical on both platforms; only the call
  // path branches. Errors here are swallowed and surfaced as null so the
  // caller can decide whether to skip the boardImage forward without a
  // visible error.
  useImperativeHandle(
    collageCaptureRef,
    () => ({
      capture: async (): Promise<string | null> => {
        try {
          if (Platform.OS === 'web') {
            // react-native-web's View renders as a div; its ref is the DOM
            // element, which html-to-image can rasterize directly.
            const node = webNodeRef.current as unknown as HTMLElement | null;
            if (!node) return null;
            return await toPng(node, { pixelRatio: 2, cacheBust: true });
          }
          const shot = viewShotRef.current;
          if (!shot || typeof shot.capture !== 'function') return null;
          const base64 = await shot.capture();
          return base64 ? `data:image/png;base64,${base64}` : null;
        } catch (err) {
          console.warn('[OutfitCard] collage capture failed', err);
          return null;
        }
      },
    }),
    [],
  );
  const cardBg = backgrounds.secondary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const tertiaryColor = labels.tertiary[colorScheme];
  const btnBg = button.primary.background[colorScheme];
  const btnText = button.primary.foreground[colorScheme];
  const thumbBg = fills.tertiary[colorScheme];
  const thumbUpActive = accents.green[colorScheme];
  const thumbDownActive = accents.red[colorScheme];
  // #23 — intentionally static white. The active thumbs background is
  // always a vivid accent colour (green / red), so the foreground must
  // be white in both light and dark modes for contrast. Using a theme
  // token like button.primary.foreground would flip to black in dark
  // mode and become unreadable against the green/red fill.
  const thumbActiveFg = '#FFFFFF';
  const showRating = Boolean(onThumbsUp || onThumbsDown);
  const isRated = rating === 'up' || rating === 'down';

  return (
    <View style={[styles.card, { width: SCREEN_WIDTH, height: cardHeight || undefined }]}>
      <View style={[styles.cardInner, { backgroundColor: cardBg }]}>
        <View style={styles.cardHeader}>
          {/* Editorial eyebrow — counter + weather/location condensed into a
              single tracking-caps line. All metadata that used to live on
              four separate rows is folded here so the title + image get the
              screen real estate. */}
          <Text style={[styles.cardEyebrow, { color: tertiaryColor }]} numberOfLines={1}>
            {buildEyebrow(index, total, weatherDetail, outfit.weather)}
          </Text>
          <Text style={[styles.cardName, { color: textColor }]} numberOfLines={1}>
            {outfit.name}
          </Text>
          {/* Archetype names + palette dots share one compact row — one
              visual signal for style, one for colour. No percentages; the
              dual-chip shape already communicates a blend. */}
          <View style={styles.chipRow}>
            <ArchetypeBadges scores={outfit.archetypeScores} colorScheme={colorScheme} />
            {outfit.palette && outfit.palette.length > 0 && (
              <View style={styles.paletteStripInline}>
                {outfit.palette.slice(0, 4).map((hex, i) => (
                  <View
                    key={`${hex}-${i}`}
                    style={[styles.paletteDot, { backgroundColor: hex, borderColor: fills.tertiary[colorScheme] }]}
                  />
                ))}
              </View>
            )}
          </View>
        </View>
        {/* Wrap the Collage so the parent can snapshot it at Save time.
            Two renderers, one Collage: native uses react-native-view-shot
            (stable, fast, native rendering); web renders inside a plain
            View whose DOM node html-to-image rasterizes. Keeping the
            Collage identical on both paths means the captured image
            matches what the user actually sees. */}
        {Platform.OS === 'web' ? (
          <View
            ref={webNodeRef}
            collapsable={false}
            style={styles.collageWrapper}
          >
            <Collage
              itemIds={outfit.items}
              itemMap={itemMap}
              layoutRoles={outfit.layoutRoles}
              visualWeights={outfit.visualWeights}
              onItemPress={onItemPress}
              colorScheme={colorScheme}
              panelUrl={outfit.panelUrl}
              backgroundUrl={outfit.backgroundUrl}
              archetypeScores={outfit.archetypeScores}
              palette={outfit.palette}
              fill
            />
          </View>
        ) : (
          <ViewShot
            ref={viewShotRef}
            options={{ format: 'png', result: 'base64', quality: 0.92 }}
            style={styles.collageWrapper}
          >
            <Collage
              itemIds={outfit.items}
              itemMap={itemMap}
              layoutRoles={outfit.layoutRoles}
              visualWeights={outfit.visualWeights}
              onItemPress={onItemPress}
              colorScheme={colorScheme}
              panelUrl={outfit.panelUrl}
              backgroundUrl={outfit.backgroundUrl}
              archetypeScores={outfit.archetypeScores}
              palette={outfit.palette}
              fill
            />
          </ViewShot>
        )}
        {outfit.rationale ? (
          <Text style={[styles.rationale, { color: tertiaryColor }]} numberOfLines={2}>
            {outfit.rationale}
          </Text>
        ) : null}
        {outfit.smartSuggestion && (
          <Text style={[styles.smartSuggestion, { color: tertiaryColor }]} numberOfLines={1}>
            Try adding: {outfit.smartSuggestion}
          </Text>
        )}
        {outfit.suggestions && outfit.suggestions.length > 0 && (
          <Text style={[styles.suggestions, { color: tertiaryColor }]} numberOfLines={2}>
            Would be nice: {outfit.suggestions.join(' · ')}
          </Text>
        )}
        <View style={styles.actionRow}>
          {showRating && (
            <>
              <Pressable
                style={[
                  styles.thumbBtn,
                  { backgroundColor: rating === 'up' ? thumbUpActive : thumbBg },
                ]}
                onPress={onThumbsUp}
                disabled={isRated || isSaving}
                hitSlop={8}
                accessibilityRole="button"
                accessibilityLabel="Rate outfit thumbs up"
                accessibilityState={{ selected: rating === 'up', disabled: isRated || isSaving }}
                testID="outfit-card-thumbs-up"
              >
                <Icon
                  name="thumbs-up"
                  size={20}
                  color={rating === 'up' ? thumbActiveFg : textColor}
                />
              </Pressable>
              <Pressable
                style={[
                  styles.thumbBtn,
                  { backgroundColor: rating === 'down' ? thumbDownActive : thumbBg },
                ]}
                onPress={onThumbsDown}
                disabled={isRated || isSaving}
                hitSlop={8}
                accessibilityRole="button"
                accessibilityLabel="Rate outfit thumbs down"
                accessibilityState={{ selected: rating === 'down', disabled: isRated || isSaving }}
                testID="outfit-card-thumbs-down"
              >
                <Icon
                  name="thumbs-down"
                  size={20}
                  color={rating === 'down' ? thumbActiveFg : textColor}
                />
              </Pressable>
            </>
          )}
          <Pressable
            style={[styles.chooseBtn, { backgroundColor: btnBg }]}
            onPress={onSelect}
            disabled={isSaving}
            accessibilityRole="button"
            accessibilityLabel="Choose this outfit"
            testID="outfit-card-choose"
          >
            {isSaving ? (
              <ActivityIndicator size="small" color={btnText} />
            ) : (
              <Text style={[styles.chooseBtnText, { color: btnText }]}>Choose this outfit</Text>
            )}
          </Pressable>
        </View>
      </View>
    </View>
  );
};

// Custom comparator for the memo wrapper. Only re-render when the data
// that actually affects what's drawn changes — not when callback
// references churn. Callbacks captured by the parent's inline
// renderItem get new references on every parent re-render; treating
// them as "same" because they semantically are (same handler, same
// closure target) is the whole point of the optimisation.
const outfitCardPropsAreEqual = (prev: OutfitCardProps, next: OutfitCardProps): boolean =>
  prev.outfit === next.outfit &&
  prev.index === next.index &&
  prev.total === next.total &&
  prev.itemMap === next.itemMap &&
  prev.isSaving === next.isSaving &&
  prev.colorScheme === next.colorScheme &&
  prev.cardHeight === next.cardHeight &&
  prev.rating === next.rating &&
  prev.weatherDetail === next.weatherDetail;

export const OutfitCard = React.memo(OutfitCardBase, outfitCardPropsAreEqual);

const styles = StyleSheet.create({
  card: {
    // width + height set inline; full dimensions for FlatList paging
    overflow: 'hidden',
  },
  cardInner: {
    marginHorizontal: CONTAINER_PADDING,
    borderRadius: 20,
    padding: 14,
    gap: 10,
    overflow: 'hidden',
    flex: 1,
    width: '100%',
    maxWidth: MAX_CARD_WIDTH,
    alignSelf: 'center',
  },
  cardHeader: {
    gap: 6,
  },
  chipRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
    alignItems: 'center',
  },
  // The ViewShot wrapper inherits the Collage's flex sizing, so the render
  // captures exactly what the user sees on the card — no extra padding or
  // layout drift between the on-screen and snapshotted output.
  collageWrapper: {
    flex: 1,
    width: '100%',
  },
  // Small inline colour dots that sit next to the archetype chips on the
  // same row. Smaller than the old standalone palette strip so they read
  // as an accent, not a separate UI element.
  paletteStripInline: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
    marginLeft: 4,
  },
  paletteDot: {
    width: 12,
    height: 12,
    borderRadius: 6,
    borderWidth: StyleSheet.hairlineWidth,
  },
  cardEyebrow: {
    ...typography.caption2.regular,
    letterSpacing: 1.6,
  },
  cardName: {
    ...typography.title2.semiBold,
    marginTop: 2,
  },
  actionRow: {
    marginTop: 'auto',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  thumbBtn: {
    width: 44,
    height: 44,
    borderRadius: 22,
    justifyContent: 'center',
    alignItems: 'center',
  },
  chooseBtn: {
    flex: 1,
    height: 44,
    borderRadius: 22,
    justifyContent: 'center',
    alignItems: 'center',
  },
  chooseBtnText: {
    ...typography.subheadline.semiBold,
  },
  rationale: {
    ...typography.footnote.regular,
    lineHeight: 16,
  },
  suggestions: {
    ...typography.footnote.regular,
    fontStyle: 'italic',
  },
  smartSuggestion: {
    ...typography.footnote.semiBold,
    fontStyle: 'italic',
  },
});
