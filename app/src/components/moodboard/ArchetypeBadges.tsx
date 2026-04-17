import { fills, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { radius } from '@/src/theme/radius';
import React from 'react';
import { StyleSheet, Text, View } from 'react-native';

const ARCHETYPE_LABELS: Record<string, string> = {
  ruler: 'Ruler', rebel: 'Rebel', creator: 'Creator', lover: 'Lover',
  hero: 'Hero', explorer: 'Explorer', sage: 'Sage', magician: 'Magician',
  innocent: 'Innocent', caregiver: 'Caregiver', jester: 'Jester', orphan: 'Everyman',
};

interface ArchetypeBadgesProps {
  scores?: Record<string, number>;
  colorScheme: 'light' | 'dark';
}

// computeDisplayPercents normalizes raw archetype scores into percentages that
// sum to 100 and are strictly descending — avoids the "Rebel 51% / Everyman 51%"
// tie that happens when two raw scores are close and both round to the same integer.
const computeDisplayPercents = (
  rawScores: Record<string, number>,
  topN = 2,
): Array<{ name: string; percent: number }> => {
  const filtered = Object.entries(rawScores)
    .filter(([, s]) => s > 0.05)
    .sort(([, a], [, b]) => b - a)
    .slice(0, topN);
  if (filtered.length === 0) return [];

  const sum = filtered.reduce((acc, [, s]) => acc + s, 0);
  const normalized = filtered.map(([name, s]) => ({ name, value: (s / sum) * 100 }));

  // Largest-remainder rounding so integers sum to exactly 100.
  const floors = normalized.map(n => Math.floor(n.value));
  const used = floors.reduce((a, b) => a + b, 0);
  const remainder = 100 - used;
  const byFrac = normalized
    .map((n, i) => ({ i, frac: n.value - floors[i] }))
    .sort((a, b) => b.frac - a.frac);
  const result = floors.slice();
  for (let k = 0; k < remainder; k++) result[byFrac[k % byFrac.length].i] += 1;

  // Enforce strict descending so primary always reads higher than secondary.
  for (let i = 1; i < result.length; i++) {
    if (result[i] >= result[i - 1]) {
      result[i - 1] += 1;
      result[i] -= 1;
    }
  }
  return normalized.map((n, i) => ({ name: n.name, percent: result[i] }));
};

export const ArchetypeBadges: React.FC<ArchetypeBadgesProps> = ({ scores, colorScheme }) => {
  if (!scores) return null;
  const entries = computeDisplayPercents(scores, 2);
  if (entries.length === 0) return null;

  return (
    <View style={styles.archetypeRow}>
      {entries.map(({ name, percent }) => (
        <View key={name} style={[styles.archetypeBadge, { backgroundColor: fills.tertiary[colorScheme] }]}>
          <Text style={[styles.archetypeBadgeText, { color: labels.secondary[colorScheme] }]}>
            {ARCHETYPE_LABELS[name] ?? name} {percent}%
          </Text>
        </View>
      ))}
    </View>
  );
};

const styles = StyleSheet.create({
  archetypeRow: {
    flexDirection: 'row',
    gap: 6,
  },
  archetypeBadge: {
    paddingHorizontal: 8,
    paddingVertical: 2,
    borderRadius: radius.full,
  },
  archetypeBadgeText: {
    ...typography.caption2.regular,
  },
});
