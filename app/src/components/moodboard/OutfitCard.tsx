import { backgrounds, button, fills, labels } from '@/src/theme/colors';
import { radius } from '@/src/theme/radius';
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

const formatWeatherChip = (w?: OutfitWeather): string | null => {
  if (!w) return null;
  const { temperature, condition, unit } = w;
  if (!temperature && !condition) return null;
  const icon = condition
    ? CONDITION_ICON[condition.toLowerCase()] ?? CONDITION_ICON[condition.toLowerCase().split(' ')[0]] ?? ''
    : '';
  const temp = temperature ? `${temperature}°${unit ?? ''}`.trim() : '';
  const cond = condition ?? '';
  return [icon, temp, cond].filter(Boolean).join(' ');
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
}

export const OutfitCard: React.FC<OutfitCardProps> = ({
  outfit, index, total, itemMap, onSelect, onItemPress, isSaving, colorScheme, cardHeight,
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
          {/* Editorial eyebrow — uppercase mood label + counter, all caps */}
          <Text style={[styles.cardEyebrow, { color: tertiaryColor }]}>
            LOOK   ·   {index + 1} / {total}
          </Text>
          <Text style={[styles.cardName, { color: textColor }]} numberOfLines={1}>
            {outfit.name}
          </Text>
          <View style={styles.chipRow}>
            <ArchetypeBadges scores={outfit.archetypeScores} colorScheme={colorScheme} />
            {formatWeatherChip(outfit.weather) && (
              <View style={[styles.weatherChip, { backgroundColor: fills.tertiary[colorScheme] }]}>
                <Text style={[styles.weatherChipText, { color: labels.secondary[colorScheme] }]}>
                  {formatWeatherChip(outfit.weather)}
                </Text>
              </View>
            )}
          </View>
          {outfit.palette && outfit.palette.length > 0 && (
            <View style={styles.paletteStrip}>
              {outfit.palette.slice(0, 4).map((hex, i) => (
                <View
                  key={`${hex}-${i}`}
                  style={[styles.paletteChip, { backgroundColor: hex, borderColor: fills.tertiary[colorScheme] }]}
                />
              ))}
            </View>
          )}
        </View>
        <Collage
          itemIds={outfit.items}
          itemMap={itemMap}
          layoutRoles={outfit.layoutRoles}
          onItemPress={onItemPress}
          colorScheme={colorScheme}
          panelUrl={outfit.panelUrl}
          backgroundUrl={outfit.backgroundUrl}
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
  weatherChip: {
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: radius.full,
  },
  weatherChipText: {
    ...typography.caption2.regular,
  },
  paletteStrip: {
    flexDirection: 'row',
    gap: 4,
    marginTop: 2,
  },
  paletteChip: {
    width: 18,
    height: 18,
    borderRadius: 5,
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
