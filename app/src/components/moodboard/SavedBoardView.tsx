import { backgrounds, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import type { SavedMoodBoard, WardrobeItem } from '@/src/domain';
import React from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { Collage } from '@/src/components/moodboard/Collage';
import { ArchetypeBadges } from '@/src/components/moodboard/ArchetypeBadges';
import { MAX_CARD_WIDTH } from '@/src/components/moodboard/constants';

export interface SavedBoardViewProps {
  board: SavedMoodBoard;
  itemMap: Map<string, WardrobeItem>;
  colorScheme: 'light' | 'dark';
  onRegenerate: () => void;
}

export const SavedBoardView: React.FC<SavedBoardViewProps> = ({ board, itemMap, colorScheme, onRegenerate }) => {
  const textColor = labels.primary[colorScheme];
  const secondaryColor = labels.secondary[colorScheme];
  const cardBg = backgrounds.secondary[colorScheme];
  const today = new Date().toISOString().split('T')[0];

  return (
    <ScrollView
      style={[styles.savedCard, { backgroundColor: cardBg }]}
      contentContainerStyle={styles.savedCardContent}
      showsVerticalScrollIndicator={false}
    >
      <View style={styles.savedHeader}>
        <Text style={[styles.savedDate, { color: secondaryColor }]}>
          {board.date === today ? "TODAY'S OUTFIT" : board.date}
        </Text>
        <Text style={[styles.savedName, { color: textColor }]}>{board.outfit.name}</Text>
        <ArchetypeBadges scores={board.outfit.archetypeScores} colorScheme={colorScheme} />
      </View>
      <Collage
        itemIds={board.outfit.items}
        itemMap={itemMap}
        snapshots={board.outfit.snapshots}
        layoutRoles={board.outfit.layoutRoles}
        colorScheme={colorScheme}
        panelUrl={board.outfit.panelUrl}
        backgroundUrl={board.outfit.backgroundUrl}
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
          <Text style={[styles.regenerateText, { color: secondaryColor }]}>Generate new outfit</Text>
        </Pressable>
      </View>
    </ScrollView>
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
  },
  savedCardContent: {
    gap: 10,
  },
  savedHeader: {
    gap: 2,
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
