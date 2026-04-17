import { backgrounds, button, fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import type { Outfit, OutfitWeather, WardrobeItem } from '@/src/domain';
import React from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { Collage } from '@/src/components/moodboard/Collage';
import { ArchetypeBadges } from '@/src/components/moodboard/ArchetypeBadges';
import { SCREEN_WIDTH, CONTAINER_PADDING, MAX_CARD_WIDTH } from '@/src/components/moodboard/constants';

const CONDITION_ICON: Record<string, string> = {
  clear: '☀', sunny: '☀', sun: '☀',
  cloud: '☁', cloudy: '☁', overcast: '☁',
  rain: '☂', rainy: '☂', drizzle: '☂', shower: '☂',
  snow: '❄', snowy: '❄', sleet: '❄',
  storm: '⚡', thunder: '⚡',
  fog: '☷', foggy: '☷', mist: '☷', haze: '☷',
  wind: '≈', windy: '≈',
};

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
  const icon = weather?.condition
    ? CONDITION_ICON[weather.condition.toLowerCase()] ??
      CONDITION_ICON[weather.condition.toLowerCase().split(' ')[0]] ??
      ''
    : '';
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
}

export const OutfitCard: React.FC<OutfitCardProps> = ({
  outfit, index, total, itemMap, onSelect, onItemPress, isSaving, colorScheme, cardHeight, weatherDetail,
}) => {
  const cardBg = backgrounds.secondary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const tertiaryColor = labels.tertiary[colorScheme];
  const btnBg = button.primary.background[colorScheme];
  const btnText = button.primary.foreground[colorScheme];

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
        <Collage
          itemIds={outfit.items}
          itemMap={itemMap}
          layoutRoles={outfit.layoutRoles}
          onItemPress={onItemPress}
          colorScheme={colorScheme}
          panelUrl={outfit.panelUrl}
          backgroundUrl={outfit.backgroundUrl}
          fill
        />
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
        <Pressable
          style={[styles.chooseBtn, { backgroundColor: btnBg }]}
          onPress={onSelect}
          disabled={isSaving}
        >
          {isSaving ? (
            <ActivityIndicator size="small" color={btnText} />
          ) : (
            <Text style={[styles.chooseBtnText, { color: btnText }]}>Choose this outfit</Text>
          )}
        </Pressable>
      </View>
    </View>
  );
};

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
  chooseBtn: {
    marginTop: 'auto',
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
