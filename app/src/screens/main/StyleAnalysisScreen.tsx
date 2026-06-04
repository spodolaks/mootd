import React, { useState, useCallback } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useFocusEffect } from '@react-navigation/native';
import { Icon } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { wardrobeRepository } from '@/src/data/repositories';
import type { WardrobeItem } from '@/src/domain';
import { backgrounds, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { spacing } from '@/src/theme/spacing';
import { radius } from '@/src/theme/radius';
import type { IconName } from '@/src/components/icons/Icon';

// ── Archetype definitions ────────────────────────────────────────────

interface ArchetypeProfile {
  name: string;
  icon: IconName;
  title: string;
  description: string;
  colorSignals: string[];
  materialSignals: string[];
  formalityRange: string[];
  keyTraits: string[];
}

const ARCHETYPE_PROFILES: Record<string, ArchetypeProfile> = {
  ruler: {
    name: 'ruler',
    icon: 'star',
    title: 'The Ruler',
    description:
      "Authority through refinement. You dress with intention — every piece signals control, quality, and status. Premium materials, structured silhouettes, and power colors define your wardrobe. You don't follow trends; you set the standard.",
    colorSignals: ['black', 'navy', 'camel', 'charcoal', 'gold', 'royal blue', 'burgundy'],
    materialSignals: ['wool', 'leather', 'silk', 'cashmere'],
    formalityRange: ['business_casual', 'business', 'formal'],
    keyTraits: [
      'Structured silhouettes',
      'Investment pieces',
      'Power palette',
      'Status accessories',
    ],
  },
  rebel: {
    name: 'rebel',
    icon: 'compass',
    title: 'The Rebel',
    description:
      'Rules are suggestions. You mix registers — leather with tailoring, streetwear with luxury — because you understand the codes well enough to break them. Edge without effort.',
    colorSignals: ['black', 'charcoal', 'dark', 'white', 'red'],
    materialSignals: ['leather', 'denim', 'metal', 'cotton twill'],
    formalityRange: ['casual', 'smart_casual'],
    keyTraits: ['Mixed registers', 'Leather as statement', 'Bold jewelry', 'Utility meets luxury'],
  },
  creator: {
    name: 'creator',
    icon: 'edit',
    title: 'The Creator',
    description:
      'Fashion as self-expression. You gravitate toward unexpected combinations, distinctive silhouettes, and pieces that tell a story. Your wardrobe is a curated gallery.',
    colorSignals: ['red', 'purple', 'cobalt', 'multi', 'mustard', 'emerald'],
    materialSignals: ['silk', 'textured knit', 'linen', 'mixed fabrics'],
    formalityRange: ['casual', 'smart_casual', 'creative_formal'],
    keyTraits: [
      'Pattern mixing',
      'Distinctive silhouettes',
      'Statement accessories',
      'Curated color stories',
    ],
  },
  lover: {
    name: 'lover',
    icon: 'sunrise',
    title: 'The Lover',
    description:
      'Sensuality in fabric. You choose pieces that feel as good as they look — soft textures, elegant draping, and a palette that flatters. Romance lives in the details.',
    colorSignals: ['blush', 'rose', 'cream', 'soft pink', 'burgundy', 'champagne'],
    materialSignals: ['silk', 'satin', 'cashmere', 'velvet', 'lace'],
    formalityRange: ['smart_casual', 'creative_formal', 'formal'],
    keyTraits: ['Luxe textures', 'Elegant drape', 'Romantic palette', 'Refined details'],
  },
  hero: {
    name: 'hero',
    icon: 'check',
    title: 'The Hero',
    description:
      'Dressed for action. Clean lines, functional materials, and a wardrobe built to perform. You favor reliability and confidence over flash.',
    colorSignals: ['navy', 'white', 'black', 'steel', 'forest green'],
    materialSignals: ['cotton', 'performance fabric', 'structured wool', 'nylon'],
    formalityRange: ['casual', 'smart_casual', 'business_casual'],
    keyTraits: ['Clean functionality', 'Reliable fits', 'Performance-ready', 'Confident basics'],
  },
  explorer: {
    name: 'explorer',
    icon: 'compass',
    title: 'The Explorer',
    description:
      'Freedom in movement. Your wardrobe is built for versatility — layers that adapt, materials that travel, and colors drawn from the world around you.',
    colorSignals: ['earth', 'olive', 'tan', 'rust', 'sky blue', 'sand'],
    materialSignals: ['canvas', 'denim', 'cotton', 'linen', 'suede'],
    formalityRange: ['casual', 'smart_casual'],
    keyTraits: ['Versatile layering', 'Natural palette', 'Travel-ready', 'Rugged textures'],
  },
  sage: {
    name: 'sage',
    icon: 'idea',
    title: 'The Sage',
    description:
      'Quiet intelligence. You dress in a way that never distracts — timeless pieces, muted palettes, and an understated confidence that speaks through restraint.',
    colorSignals: ['charcoal', 'grey', 'navy', 'forest', 'ivory', 'slate'],
    materialSignals: ['wool', 'cotton', 'tweed', 'fine knit'],
    formalityRange: ['smart_casual', 'business_casual', 'business'],
    keyTraits: [
      'Timeless silhouettes',
      'Muted palette',
      'Quality over quantity',
      'Intellectual restraint',
    ],
  },
  magician: {
    name: 'magician',
    icon: 'moon',
    title: 'The Magician',
    description:
      'Transformation through style. You curate an aura — dark tones, unexpected textures, and layered compositions that reveal depth the longer someone looks.',
    colorSignals: ['black', 'deep purple', 'midnight', 'charcoal', 'dark green'],
    materialSignals: ['silk', 'velvet', 'sheer', 'textured', 'leather'],
    formalityRange: ['smart_casual', 'creative_formal'],
    keyTraits: ['Dark palette', 'Layered depth', 'Textural contrast', 'Mysterious aura'],
  },
  innocent: {
    name: 'innocent',
    icon: 'sun',
    title: 'The Innocent',
    description:
      'Effortless simplicity. Clean whites, soft fabrics, and uncomplicated shapes. Your style radiates optimism and approachability without trying.',
    colorSignals: ['white', 'light blue', 'pastel', 'cream', 'soft yellow'],
    materialSignals: ['cotton', 'linen', 'jersey', 'chambray'],
    formalityRange: ['casual', 'smart_casual'],
    keyTraits: ['Clean whites', 'Simple shapes', 'Soft fabrics', 'Effortless freshness'],
  },
  caregiver: {
    name: 'caregiver',
    icon: 'help',
    title: 'The Caregiver',
    description:
      'Warmth you can wear. Soft textures, approachable colors, and comfortable fits that make everyone around you feel at ease. Function and feeling over fashion.',
    colorSignals: ['light blue', 'lavender', 'cream', 'soft pink', 'warm grey'],
    materialSignals: ['cotton', 'jersey', 'soft knit', 'linen', 'fleece'],
    formalityRange: ['casual', 'smart_casual'],
    keyTraits: ['Comfort-first', 'Soft palette', 'Approachable textures', 'Relaxed fits'],
  },
  jester: {
    name: 'jester',
    icon: 'idea',
    title: 'The Jester',
    description:
      "Life's too short for boring clothes. Bold prints, playful colors, and unexpected combinations that make people smile. Your wardrobe is a conversation starter.",
    colorSignals: ['bright', 'yellow', 'orange', 'electric blue', 'hot pink', 'multi'],
    materialSignals: ['printed', 'graphic tee', 'novelty', 'denim'],
    formalityRange: ['casual'],
    keyTraits: ['Bold prints', 'Playful color', 'Conversation pieces', 'Fearless combos'],
  },
  orphan: {
    name: 'orphan',
    icon: 'user',
    title: 'The Everyman',
    description:
      'Belonging through relatability. You dress to connect, not to stand out — versatile basics, dependable fits, and a wardrobe that works everywhere. Authenticity over affectation.',
    colorSignals: ['blue', 'grey', 'khaki', 'white', 'brown'],
    materialSignals: ['cotton', 'denim', 'jersey', 'chino'],
    formalityRange: ['casual', 'smart_casual'],
    keyTraits: ['Versatile basics', 'Dependable fits', 'Neutral palette', 'Approachable style'],
  },
};

// ── Scoring engine ───────────────────────────────────────────────────

interface ScoredArchetype {
  name: string;
  profile: ArchetypeProfile;
  score: number;
  matchedSignals: string[];
}

function analyzeWardrobe(items: WardrobeItem[]): ScoredArchetype[] {
  // Collect all signals from the wardrobe.
  const allColors: string[] = [];
  const allMaterials: string[] = [];
  const allStyles: string[] = [];
  const allOccasions: string[] = [];
  const allDetails: string[] = [];

  for (const item of items) {
    const t = item.traits;
    if (t.color) allColors.push(t.color.toLowerCase());
    if (t.color_secondary) allColors.push(t.color_secondary.toLowerCase());
    if (t.fabric) allMaterials.push(t.fabric.toLowerCase());
    if (t.style) allStyles.push(t.style.toLowerCase());
    if (t.occasion) allOccasions.push(t.occasion.toLowerCase());
    if (t.details) allDetails.push(t.details.toLowerCase());
    if (t.overall_style) allStyles.push(t.overall_style.toLowerCase());
  }

  const allSignals = [
    ...allColors,
    ...allMaterials,
    ...allStyles,
    ...allOccasions,
    ...allDetails,
  ].join(' ');

  const results: ScoredArchetype[] = [];

  for (const [key, profile] of Object.entries(ARCHETYPE_PROFILES)) {
    let score = 0;
    const matchedSignals: string[] = [];

    // Color signals (weight: 0.3)
    let colorHits = 0;
    for (const signal of profile.colorSignals) {
      if (allColors.some(c => c.includes(signal))) {
        colorHits++;
        matchedSignals.push(signal);
      }
    }
    score += (colorHits / Math.max(profile.colorSignals.length, 1)) * 0.3;

    // Material signals (weight: 0.25)
    let materialHits = 0;
    for (const signal of profile.materialSignals) {
      if (allMaterials.some(m => m.includes(signal))) {
        materialHits++;
        matchedSignals.push(signal);
      }
    }
    score += (materialHits / Math.max(profile.materialSignals.length, 1)) * 0.25;

    // Formality signals (weight: 0.2)
    let formalityHits = 0;
    for (const f of profile.formalityRange) {
      if (allSignals.includes(f.replace('_', ' '))) formalityHits++;
    }
    score += (formalityHits / Math.max(profile.formalityRange.length, 1)) * 0.2;

    // Style/occasion keyword match (weight: 0.15)
    const styleKeywords = [...profile.keyTraits.map(t => t.toLowerCase().split(' ')).flat()];
    let styleHits = 0;
    for (const kw of styleKeywords) {
      if (allSignals.includes(kw)) styleHits++;
    }
    score += (styleHits / Math.max(styleKeywords.length, 1)) * 0.15;

    // Accessory signals (weight: 0.1)
    const accessories = items.filter(i => i.category === 'accessory');
    if (accessories.length > 0) {
      const accessoryIntensity = Math.min(accessories.length / items.length, 1);
      // High accessory intensity favors ruler, rebel, creator, magician
      if (['ruler', 'rebel', 'creator', 'magician', 'lover'].includes(key)) {
        score += accessoryIntensity * 0.1;
      }
    }

    results.push({ name: key, profile, score, matchedSignals });
  }

  return results.sort((a, b) => b.score - a.score);
}

// ── Component ────────────────────────────────────────────────────────

export const StyleAnalysisScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const [items, setItems] = useState<WardrobeItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const tertiaryText = labels.tertiary[colorScheme];
  const cardBg = grays.gray5[colorScheme];

  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      const load = async () => {
        setIsLoading(true);
        try {
          // getAllItems — Style Analysis aggregates archetype
          // affinity across the full wardrobe, so missing items
          // past the first page would skew the breakdown.
          const result = await wardrobeRepository.getAllItems();
          if (!cancelled) setItems(result);
        } catch {
          // silently fail
        } finally {
          if (!cancelled) setIsLoading(false);
        }
      };
      void load();
      return () => {
        cancelled = true;
      };
    }, [])
  );

  const scored = analyzeWardrobe(items);
  const primary = scored[0];
  const secondary = scored[1];
  const tertiary = scored[2];
  const hasEnoughItems = items.length >= 3;

  if (isLoading) {
    return (
      <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
        <View style={styles.header}>
          <Text style={[styles.title, { color: textColor }]}>Style Analysis</Text>
        </View>
        <View style={styles.centered}>
          <ActivityIndicator size="large" color={textColor} />
        </View>
      </SafeAreaView>
    );
  }

  if (!hasEnoughItems) {
    return (
      <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
        <View style={styles.header}>
          <Text style={[styles.title, { color: textColor }]}>Style Analysis</Text>
        </View>
        <View style={styles.centered}>
          <Icon name="closet" size={48} color={tertiaryText} />
          <Text style={[styles.emptyText, { color: tertiaryText }]}>
            Add at least 3 items to your wardrobe{'\n'}to unlock style analysis
          </Text>
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={styles.header}>
        <Text style={[styles.title, { color: textColor }]}>Style Analysis</Text>
        <Text style={[styles.subtitle, { color: secondaryText }]}>
          Based on {items.length} wardrobe items
        </Text>
      </View>

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}>
        {/* Primary archetype — hero card */}
        {primary && (
          <View style={[styles.heroCard, { backgroundColor: cardBg }]}>
            <View style={styles.heroHeader}>
              <Icon name={primary.profile.icon} size={28} color={textColor} />
              <Text style={[styles.heroTitle, { color: textColor }]}>{primary.profile.title}</Text>
            </View>
            <View style={styles.heroScoreBadge}>
              <Text style={[styles.heroScoreLabel, { color: secondaryText }]}>
                Primary archetype
              </Text>
              <Text style={[styles.heroScoreValue, { color: textColor }]}>
                {Math.round(primary.score * 100)}% match
              </Text>
            </View>
            <Text style={[styles.heroDescription, { color: secondaryText }]}>
              {primary.profile.description}
            </Text>
            <View style={styles.traitRow}>
              {primary.profile.keyTraits.map(trait => (
                <View
                  key={trait}
                  style={[styles.traitChip, { backgroundColor: backgrounds.primary[colorScheme] }]}>
                  <Text style={[styles.traitChipText, { color: textColor }]}>{trait}</Text>
                </View>
              ))}
            </View>
          </View>
        )}

        {/* Secondary + Tertiary */}
        {secondary && (
          <View style={styles.secondaryRow}>
            <ArchetypeCard
              archetype={secondary}
              label="Secondary"
              colorScheme={colorScheme}
              cardBg={cardBg}
              textColor={textColor}
              secondaryText={secondaryText}
            />
            {tertiary && (
              <ArchetypeCard
                archetype={tertiary}
                label="Tertiary"
                colorScheme={colorScheme}
                cardBg={cardBg}
                textColor={textColor}
                secondaryText={secondaryText}
              />
            )}
          </View>
        )}

        {/* Wardrobe signals breakdown */}
        <View style={[styles.signalsCard, { backgroundColor: cardBg }]}>
          <Text style={[styles.sectionTitle, { color: textColor }]}>Detected signals</Text>

          <SignalRow
            label="Dominant colors"
            values={[...new Set(items.map(i => i.traits.color).filter(Boolean))].slice(0, 5)}
            textColor={textColor}
            secondaryText={secondaryText}
            chipBg={backgrounds.primary[colorScheme]}
          />
          <SignalRow
            label="Key materials"
            values={[...new Set(items.map(i => i.traits.fabric).filter(Boolean))].slice(0, 5)}
            textColor={textColor}
            secondaryText={secondaryText}
            chipBg={backgrounds.primary[colorScheme]}
          />
          <SignalRow
            label="Style direction"
            values={[...new Set(items.map(i => i.traits.style).filter(Boolean))].slice(0, 4)}
            textColor={textColor}
            secondaryText={secondaryText}
            chipBg={backgrounds.primary[colorScheme]}
          />
        </View>

        {/* Full ranking */}
        <View style={[styles.rankingCard, { backgroundColor: cardBg }]}>
          <Text style={[styles.sectionTitle, { color: textColor }]}>All archetypes</Text>
          {scored.map((s, i) => (
            <View key={s.name} style={styles.rankingRow}>
              <Text style={[styles.rankingPosition, { color: tertiaryText }]}>{i + 1}</Text>
              <Icon name={s.profile.icon} size={16} color={secondaryText} />
              <Text style={[styles.rankingName, { color: textColor }]}>{s.profile.title}</Text>
              <View style={styles.rankingBarContainer}>
                <View
                  style={[
                    styles.rankingBar,
                    {
                      width: `${Math.round(s.score * 100)}%`,
                      backgroundColor: i === 0 ? textColor : secondaryText,
                      opacity: i === 0 ? 1 : 0.4,
                    },
                  ]}
                />
              </View>
              <Text style={[styles.rankingScore, { color: secondaryText }]}>
                {Math.round(s.score * 100)}%
              </Text>
            </View>
          ))}
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

// ── Sub-components ───────────────────────────────────────────────────

interface ArchetypeCardProps {
  archetype: ScoredArchetype;
  label: string;
  colorScheme: 'light' | 'dark';
  cardBg: string;
  textColor: string;
  secondaryText: string;
}

const ArchetypeCard: React.FC<ArchetypeCardProps> = ({
  archetype,
  label,
  cardBg,
  textColor,
  secondaryText,
}) => (
  <View style={[styles.miniCard, { backgroundColor: cardBg }]}>
    <Text style={[styles.miniLabel, { color: secondaryText }]}>{label}</Text>
    <Icon name={archetype.profile.icon} size={20} color={textColor} />
    <Text style={[styles.miniTitle, { color: textColor }]}>{archetype.profile.title}</Text>
    <Text style={[styles.miniScore, { color: secondaryText }]}>
      {Math.round(archetype.score * 100)}%
    </Text>
  </View>
);

interface SignalRowProps {
  label: string;
  values: string[];
  textColor: string;
  secondaryText: string;
  chipBg: string;
}

const SignalRow: React.FC<SignalRowProps> = ({
  label,
  values,
  textColor,
  secondaryText,
  chipBg,
}) => (
  <View style={styles.signalRow}>
    <Text style={[styles.signalLabel, { color: secondaryText }]}>{label}</Text>
    <View style={styles.signalChips}>
      {values.map(v => (
        <View key={v} style={[styles.traitChip, { backgroundColor: chipBg }]}>
          <Text style={[styles.traitChipText, { color: textColor }]}>{v}</Text>
        </View>
      ))}
    </View>
  </View>
);

// ── Styles ───────────────────────────────────────────────────────────

const styles = StyleSheet.create({
  container: { flex: 1 },
  header: {
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  title: { ...typography.largeTitle.semiBold },
  subtitle: { ...typography.subheadline.regular, marginTop: 2 },
  scrollView: { flex: 1 },
  scrollContent: {
    paddingHorizontal: spacing.lg,
    paddingBottom: 40,
    gap: spacing.md,
  },
  centered: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    gap: spacing.md,
  },
  emptyText: {
    ...typography.subheadline.regular,
    textAlign: 'center',
    lineHeight: 22,
  },

  // Hero card (primary archetype)
  heroCard: {
    borderRadius: radius.xl,
    padding: spacing.lg,
    gap: spacing.sm,
  },
  heroHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  heroTitle: { ...typography.title2.semiBold },
  heroScoreBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  heroScoreLabel: { ...typography.footnote.regular },
  heroScoreValue: { ...typography.footnote.semiBold },
  heroDescription: {
    ...typography.subheadline.regular,
    lineHeight: 22,
  },
  traitRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
    marginTop: spacing.xs,
  },
  traitChip: {
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: radius.full,
  },
  traitChipText: { ...typography.caption1.regular },

  // Secondary cards row
  secondaryRow: {
    flexDirection: 'row',
    gap: spacing.sm,
  },
  miniCard: {
    flex: 1,
    borderRadius: radius.xl,
    padding: spacing.md,
    alignItems: 'center',
    gap: spacing.xs,
  },
  miniLabel: { ...typography.caption2.regular },
  miniTitle: { ...typography.footnote.semiBold, textAlign: 'center' },
  miniScore: { ...typography.caption1.regular },

  // Signals card
  signalsCard: {
    borderRadius: radius.xl,
    padding: spacing.lg,
    gap: spacing.md,
  },
  sectionTitle: { ...typography.headline.semiBold },
  signalRow: { gap: spacing.xs },
  signalLabel: { ...typography.footnote.regular },
  signalChips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.xs,
  },

  // Ranking card
  rankingCard: {
    borderRadius: radius.xl,
    padding: spacing.lg,
    gap: spacing.sm,
  },
  rankingRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    height: 28,
  },
  rankingPosition: {
    ...typography.caption1.regular,
    width: 16,
    textAlign: 'center',
  },
  rankingName: {
    ...typography.footnote.regular,
    width: 100,
  },
  rankingBarContainer: {
    flex: 1,
    height: 4,
    borderRadius: 2,
    backgroundColor: 'rgba(142,142,147,0.15)',
  },
  rankingBar: {
    height: 4,
    borderRadius: 2,
  },
  rankingScore: {
    ...typography.caption1.regular,
    width: 32,
    textAlign: 'right',
  },
});
