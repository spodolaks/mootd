import { backgrounds, fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import type { SavedMoodBoard, WardrobeItem } from '@/src/domain';
import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Collage } from '@/src/components/moodboard/Collage';
import { ArchetypeBadges } from '@/src/components/moodboard/ArchetypeBadges';
import { MAX_CARD_WIDTH } from '@/src/components/moodboard/constants';
import { formatWeatherChip } from '@/src/components/moodboard/weatherChip';

export interface SavedBoardViewProps {
  board: SavedMoodBoard;
  itemMap: Map<string, WardrobeItem>;
  colorScheme: 'light' | 'dark';
  onRegenerate: () => void;
}

export const SavedBoardView: React.FC<SavedBoardViewProps> = ({
  board,
  itemMap,
  colorScheme,
  onRegenerate,
}) => {
  const textColor = labels.primary[colorScheme];
  const secondaryColor = labels.secondary[colorScheme];
  const tertiaryColor = labels.tertiary[colorScheme];
  const cardBg = backgrounds.secondary[colorScheme];
  const today = new Date().toISOString().split('T')[0];
  const weatherChip = formatWeatherChip(board.outfit.weather);
  const palette = board.outfit.palette;

  // Flex column layout (not ScrollView) so the Collage can fill the entire
  // height remaining between the header and footer. Header + footer are
  // intrinsic-height; the collage grows to absorb the rest of the card.
  return (
    <View style={[styles.savedCard, { backgroundColor: cardBg }]}>
      <View style={styles.savedHeader}>
        <Text style={[styles.savedDate, { color: secondaryColor }]}>
          {board.date === today ? "TODAY'S OUTFIT" : board.date}
        </Text>
        <Text style={[styles.savedName, { color: textColor }]}>{board.outfit.name}</Text>
        <View style={styles.chipRow}>
          <ArchetypeBadges scores={board.outfit.archetypeScores} colorScheme={colorScheme} />
          {palette && palette.length > 0 && (
            <View style={styles.paletteStripInline}>
              {palette.slice(0, 4).map((hex, i) => (
                <View
                  key={`${hex}-${i}`}
                  style={[
                    styles.paletteDot,
                    { backgroundColor: hex, borderColor: fills.tertiary[colorScheme] },
                  ]}
                />
              ))}
            </View>
          )}
        </View>
        {weatherChip && (
          <Text style={[styles.weatherChip, { color: tertiaryColor }]} numberOfLines={1}>
            {weatherChip}
          </Text>
        )}
      </View>
      <Collage
        itemIds={board.outfit.items}
        itemMap={itemMap}
        snapshots={board.outfit.snapshots}
        layoutRoles={board.outfit.layoutRoles}
        visualWeights={board.outfit.visualWeights}
        colorScheme={colorScheme}
        panelUrl={board.outfit.panelUrl}
        backgroundUrl={board.outfit.backgroundUrl}
        archetypeScores={board.outfit.archetypeScores}
        palette={board.outfit.palette}
        fill
      />
      <View style={styles.savedFooter}>
        <Text style={[styles.savedDescription, { color: secondaryColor }]} numberOfLines={2}>
          {board.outfit.description}
        </Text>
        {board.outfit.rationale ? (
          <Text style={[styles.rationale, { color: secondaryColor }]} numberOfLines={2}>
            {board.outfit.rationale}
          </Text>
        ) : null}
        {board.outfit.suggestions && board.outfit.suggestions.length > 0 && (
          <Text style={[styles.suggestions, { color: secondaryColor }]} numberOfLines={2}>
            Would be nice: {board.outfit.suggestions.join(' · ')}
          </Text>
        )}
        <Pressable onPress={onRegenerate} style={styles.regenerateBtn}>
          <Text style={[styles.regenerateText, { color: secondaryColor }]}>
            Generate new outfit
          </Text>
        </Pressable>
      </View>
    </View>
  );
};

const styles = StyleSheet.create({
  savedCard: {
    flex: 1,
    borderRadius: 20,
    padding: 16,
    width: '100%',
    maxWidth: MAX_CARD_WIDTH,
    alignSelf: 'center',
    gap: 10,
  },
  savedHeader: {
    gap: 6,
  },
  chipRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
    alignItems: 'center',
  },
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
  weatherChip: {
    ...typography.caption2.regular,
    letterSpacing: 1.2,
  },
  savedDate: {
    ...typography.caption2.regular,
    letterSpacing: 1.6,
  },
  savedName: {
    ...typography.title2.semiBold,
    marginTop: 2,
  },
  savedFooter: {
    gap: 6,
  },
  savedDescription: {
    ...typography.subheadline.regular,
    lineHeight: 20,
  },
  regenerateBtn: {
    alignItems: 'center',
    paddingVertical: 4,
  },
  regenerateText: {
    ...typography.subheadline.regular,
    textDecorationLine: 'underline',
  },
  rationale: {
    ...typography.footnote.regular,
    lineHeight: 16,
  },
  suggestions: {
    ...typography.footnote.regular,
    fontStyle: 'italic',
  },
});
