import { Icon } from '@/src/components';
import { labels } from '@/src/theme/colors';
import { grays } from '@/src/theme/colors';
import type { OutfitItem, WardrobeItem } from '@/src/domain';
import React, { useMemo } from 'react';
import { Platform, Pressable, StyleSheet, View } from 'react-native';
import { Image } from 'expo-image';

// Local surface library. Each entry pairs a bundled asset with the same
// archetype-affinity map the backend stores in MongoDB (see
// app/assets/images/*/<name>.json). The client carries this copy so that
// when the backend-picked panelUrl / backgroundUrl is missing (offline dev,
// outfits generated before the surface feature, a failed LLM response) we
// can still pick a surface that complements the outfit — using the same
// scoring logic the server would.
//
// Metro requires static require() arguments, so every asset must be
// enumerated. Keep this list in sync with app/assets/images/panels/ and
// app/assets/images/backgrounds/ — add a line per new surface.
type ArchetypeAffinity = Readonly<Record<string, number>>;
type LocalSurface = { source: number; affinity: ArchetypeAffinity };

const LOCAL_PANELS: readonly LocalSurface[] = [
  { source: require('../../../assets/images/panels/L2-Surface-Concrete.png'),          affinity: { explorer: 0.8, outlaw: 0.7, creator: 0.4 } },
  { source: require('../../../assets/images/panels/L2-Surface-Dark-Asphalt.png'),      affinity: { outlaw: 0.9, explorer: 0.6 } },
  { source: require('../../../assets/images/panels/L2-Surface-Light-Stone-Table.png'), affinity: { ruler: 0.7, sage: 0.6, lover: 0.4 } },
  { source: require('../../../assets/images/panels/L2-Surface-Linen.png'),             affinity: { lover: 0.8, innocent: 0.7, caregiver: 0.5 } },
  { source: require('../../../assets/images/panels/L2-Surface-Marble.png'),            affinity: { ruler: 0.9, lover: 0.6, sage: 0.4 } },
  { source: require('../../../assets/images/panels/L2-Surface-Off-white-Rainbow.png'), affinity: { creator: 0.8, jester: 0.6, innocent: 0.5 } },
  { source: require('../../../assets/images/panels/L2-Surface-Studio-floor.png'),      affinity: { creator: 0.7, sage: 0.6, magician: 0.4 } },
  { source: require('../../../assets/images/panels/L2-Surface-Urban-Pavement.png'),    affinity: { explorer: 0.8, everyman: 0.6, outlaw: 0.4 } },
  { source: require('../../../assets/images/panels/L2-Surface-Wet-asphalt.png'),       affinity: { outlaw: 0.8, explorer: 0.6, rebel: 0.5 } },
  { source: require('../../../assets/images/panels/L2-Surface-Wooden-floor.png'),      affinity: { everyman: 0.7, caregiver: 0.6, sage: 0.4 } },
];

const LOCAL_BACKGROUNDS: readonly LocalSurface[] = [
  { source: require('../../../assets/images/backgrounds/L3-Place-Cafe.png'),         affinity: { everyman: 0.7, lover: 0.5, jester: 0.4 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-City-Street.png'),  affinity: { explorer: 0.8, rebel: 0.6, outlaw: 0.4 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Green-Park.png'),   affinity: { innocent: 0.7, caregiver: 0.6, sage: 0.4 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Hotel-Lobby.png'),  affinity: { ruler: 0.8, lover: 0.5 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Morning-Room.png'), affinity: { lover: 0.7, innocent: 0.6, caregiver: 0.4 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Night-City.png'),   affinity: { outlaw: 0.8, explorer: 0.6, magician: 0.5 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Office-Window.png'), affinity: { ruler: 0.7, sage: 0.6, creator: 0.5 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Office.png'),       affinity: { ruler: 0.7, sage: 0.6, creator: 0.5 } },
  { source: require('../../../assets/images/backgrounds/L3-Place-Wet-City.png'),     affinity: { outlaw: 0.7, explorer: 0.6, rebel: 0.4 } },
];

// Deterministic hash — used both for stable tiebreaks in affinity scoring
// and as a last-resort seed when the outfit carries no archetype data.
// Hash (not Math.random) means the same outfit keeps the same surface
// across re-renders, so there's no flicker when the card re-mounts.
const hashSeed = (seed: string): number => {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = ((h << 5) - h + seed.charCodeAt(i)) | 0;
  return Math.abs(h);
};

// pickSurface scores each candidate by the dot product of its archetype
// affinity against the outfit's archetype scores, then returns the highest
// scorer. Ties break on the hash of the seed so the choice stays stable
// across renders and the same outfit always lands on the same surface.
//
// When scores is empty (legacy outfit, no archetype data) the scorer
// becomes a no-op and the hash does all the work — same behaviour as the
// previous random-but-deterministic pick, so we don't regress on outfits
// that pre-date the archetype-scoring feature.
const pickSurface = (
  surfaces: readonly LocalSurface[],
  scores: Readonly<Record<string, number>> | undefined,
  seed: string,
): number => {
  if (surfaces.length === 0) {
    throw new Error('pickSurface: empty surface list');
  }
  const hash = hashSeed(seed);
  let bestIdx = hash % surfaces.length;
  let bestScore = -Infinity;

  surfaces.forEach((surface, idx) => {
    let score = 0;
    if (scores) {
      for (const [archetype, weight] of Object.entries(surface.affinity)) {
        score += weight * (scores[archetype] ?? 0);
      }
    }
    // Tiny hash-derived tiebreaker keeps the pick stable + spreads
    // otherwise-identical scores across different outfits.
    score += ((hash + idx) % 997) * 1e-6;

    if (score > bestScore) {
      bestScore = score;
      bestIdx = idx;
    }
  });

  return surfaces[bestIdx].source;
};

// Shadow that traces each cutout's alpha channel rather than its bounding box.
// - Web: stacked drop-shadows — one tight contact shadow (gives the ground-
//   plane read: "this item is resting on the panel") plus one wider ambient
//   shadow for depth. Single drop-shadow looks "floating" and flat.
// - iOS: shadowColor on a transparent container follows the CALayer contents
//   alpha; we can only set one shadow per view so numbers are tuned for a
//   blended look rather than two separate casts.
// - Android: native `elevation` draws a rectangular material shadow which looks
//   wrong on cutouts, so we skip it and leave garments unshadowed there.
const ITEM_SHADOW_STYLE = Platform.select({
  web: {
    filter:
      'drop-shadow(0 2px 3px rgba(0,0,0,0.55)) drop-shadow(0 14px 28px rgba(0,0,0,0.4))',
  },
  ios: {
    shadowColor: '#000',
    shadowOpacity: 0.55,
    shadowOffset: { width: 0, height: 10 },
    shadowRadius: 22,
    backgroundColor: 'transparent',
  },
  default: {},
}) as object;

// Shadow cast by the panel onto the background — gives the flat-lay a sense
// of being a raised surface rather than a sticker pasted on the bokeh.
const PANEL_SHADOW_STYLE = Platform.select({
  web: {
    filter:
      'drop-shadow(0 3px 6px rgba(0,0,0,0.45)) drop-shadow(0 22px 40px rgba(0,0,0,0.5))',
  },
  ios: {
    shadowColor: '#000',
    shadowOpacity: 0.55,
    shadowOffset: { width: 0, height: 14 },
    shadowRadius: 30,
  },
  default: {},
}) as object;

// ---------- Zone classification ----------

export type ItemZone = 'outerwear' | 'tops' | 'bottoms' | 'shoes' | 'accessories';

// Ordered most-specific → least-specific; unmatched defaults to 'tops'.
// Actual DB category values: "outer", "top_long", "top_sleeveless", "bottom_long",
// "footwear_pair", "accessory" — patterns must match these exact strings.
export const ZONE_PATTERNS: [ItemZone, RegExp][] = [
  ['outerwear',   /outer|jacket|blazer|coat|trench|parka|bomber/i],
  ['shoes',       /footwear|shoes?|sneaker|boot|sandal|heel|loafer|slipper|mule|oxford/i],
  ['bottoms',     /bottom|pant|jean|\bshorts?\b|skirt|legging|trouser/i],
  ['accessories', /accessor|eyewear|bag|hat|cap|tie|belt|scarf|sunglass|watch|purse|backpack|jewelry/i],
];

export const classifyZone = (category: string): ItemZone => {
  for (const [zone, re] of ZONE_PATTERNS) {
    if (re.test(category)) return zone;
  }
  return 'tops';
};

// ---------- Zone positions ----------

// Flat-lay style positions — centered vertical body flow with overlapping layers.
export type ZonePos = { l: `${number}%`; t: `${number}%`; w: `${number}%`; h: `${number}%` };

// FIVE_ZONE_POSITIONS — the default layout, assuming every slot is filled
// (outerwear + top + bottom + shoes + accessory). Outerwear leans right and
// peeks from behind the top; shoes and accessory balance the left edge.
// Positions are relative to the surface panel (not the outer collage frame).
// All items must stay within 0-100% to avoid overflowing the panel edges.
export const FIVE_ZONE_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  outerwear: [
    { l: '36%', t: '5%',  w: '55%', h: '42%' },
    { l: '30%', t: '7%',  w: '52%', h: '40%' },
    { l: '26%', t: '9%',  w: '48%', h: '38%' },
  ],
  tops: [
    { l: '14%', t: '6%',  w: '52%', h: '40%' },
    { l: '10%', t: '5%',  w: '50%', h: '38%' },
    { l: '8%',  t: '8%',  w: '46%', h: '36%' },
  ],
  bottoms: [
    { l: '18%', t: '38%', w: '50%', h: '44%' },
    { l: '14%', t: '40%', w: '48%', h: '42%' },
    { l: '12%', t: '42%', w: '44%', h: '40%' },
  ],
  shoes: [
    { l: '5%',  t: '72%', w: '28%', h: '22%' },
    { l: '5%',  t: '76%', w: '26%', h: '18%' },
    { l: '5%',  t: '78%', w: '24%', h: '16%' },
  ],
  accessories: [
    { l: '5%',  t: '5%',  w: '18%', h: '16%' },
    { l: '5%',  t: '24%', w: '16%', h: '14%' },
    { l: '5%',  t: '42%', w: '14%', h: '12%' },
  ],
};

// NO_OUTER_POSITIONS — rebalanced 4-zone layout for when outerwear is absent.
// Without the jacket to anchor the right side, the top + bottom get pulled
// toward center and the accessory + shoes move to opposite corners so the
// eye reads the whole panel instead of drifting left.
//
// Layout: accessory top-LEFT  ·  top + bottom centered  ·  shoes bottom-RIGHT
// (diagonal balance — the same recipe editorial flat-lays use when the
// outerwear slot is empty).
export const NO_OUTER_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  // unused in this layout but kept to satisfy the Record type
  outerwear: [
    { l: '30%', t: '5%',  w: '50%', h: '40%' },
    { l: '28%', t: '7%',  w: '48%', h: '38%' },
    { l: '26%', t: '9%',  w: '46%', h: '36%' },
  ],
  tops: [
    { l: '22%', t: '5%',  w: '56%', h: '44%' },
    { l: '20%', t: '6%',  w: '52%', h: '42%' },
    { l: '18%', t: '8%',  w: '48%', h: '40%' },
  ],
  bottoms: [
    { l: '26%', t: '40%', w: '48%', h: '46%' },
    { l: '24%', t: '42%', w: '46%', h: '44%' },
    { l: '22%', t: '44%', w: '44%', h: '42%' },
  ],
  shoes: [
    { l: '64%', t: '72%', w: '30%', h: '22%' },
    { l: '66%', t: '76%', w: '28%', h: '18%' },
    { l: '68%', t: '78%', w: '26%', h: '16%' },
  ],
  accessories: [
    { l: '4%',  t: '6%',  w: '20%', h: '18%' },
    { l: '4%',  t: '26%', w: '18%', h: '16%' },
    { l: '4%',  t: '44%', w: '16%', h: '14%' },
  ],
};

// NO_ACCESSORY_POSITIONS — 4 zones, accessory dropped. Outerwear stays
// right, top+bottom centered, shoes widen slightly to fill the lower edge.
export const NO_ACCESSORY_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  outerwear: [
    { l: '40%', t: '5%',  w: '55%', h: '42%' },
    { l: '34%', t: '7%',  w: '52%', h: '40%' },
    { l: '30%', t: '9%',  w: '48%', h: '38%' },
  ],
  tops: [
    { l: '8%',  t: '6%',  w: '52%', h: '40%' },
    { l: '6%',  t: '5%',  w: '50%', h: '38%' },
    { l: '4%',  t: '8%',  w: '46%', h: '36%' },
  ],
  bottoms: [
    { l: '18%', t: '38%', w: '50%', h: '44%' },
    { l: '14%', t: '40%', w: '48%', h: '42%' },
    { l: '12%', t: '42%', w: '44%', h: '40%' },
  ],
  shoes: [
    { l: '10%', t: '72%', w: '32%', h: '22%' },
    { l: '10%', t: '76%', w: '30%', h: '18%' },
    { l: '10%', t: '78%', w: '28%', h: '16%' },
  ],
  accessories: [
    { l: '5%',  t: '5%',  w: '18%', h: '16%' },
    { l: '5%',  t: '24%', w: '16%', h: '14%' },
    { l: '5%',  t: '42%', w: '14%', h: '12%' },
  ],
};

// MINIMAL_POSITIONS — 3-zone fallback (top + bottom + shoes only). Everything
// stacks centered with generous spacing so the panel doesn't look empty.
export const MINIMAL_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  outerwear: [
    { l: '30%', t: '5%',  w: '50%', h: '40%' },
    { l: '28%', t: '7%',  w: '48%', h: '38%' },
    { l: '26%', t: '9%',  w: '46%', h: '36%' },
  ],
  tops: [
    { l: '20%', t: '4%',  w: '60%', h: '46%' },
    { l: '18%', t: '6%',  w: '56%', h: '44%' },
    { l: '16%', t: '8%',  w: '52%', h: '42%' },
  ],
  bottoms: [
    { l: '26%', t: '42%', w: '48%', h: '46%' },
    { l: '24%', t: '44%', w: '46%', h: '44%' },
    { l: '22%', t: '46%', w: '44%', h: '42%' },
  ],
  shoes: [
    { l: '34%', t: '74%', w: '32%', h: '22%' },
    { l: '36%', t: '76%', w: '30%', h: '20%' },
    { l: '38%', t: '78%', w: '28%', h: '18%' },
  ],
  accessories: [
    { l: '5%',  t: '5%',  w: '18%', h: '16%' },
    { l: '5%',  t: '24%', w: '16%', h: '14%' },
    { l: '5%',  t: '42%', w: '14%', h: '12%' },
  ],
};

// Legacy export for any callers still referencing the old name.
export const ZONE_POSITIONS = FIVE_ZONE_POSITIONS;

// pickLayout returns the positions table best suited to the set of zones
// actually present on this card. Composition logic lives here so the
// render path stays a simple `positions[zone][index]` lookup.
export const pickLayout = (activeZones: Set<ItemZone>): Record<ItemZone, [ZonePos, ZonePos, ZonePos]> => {
  const hasOuter = activeZones.has('outerwear');
  const hasAccessory = activeZones.has('accessories');
  if (hasOuter && hasAccessory) return FIVE_ZONE_POSITIONS;
  if (hasOuter && !hasAccessory) return NO_ACCESSORY_POSITIONS;
  if (!hasOuter && hasAccessory) return NO_OUTER_POSITIONS;
  return MINIMAL_POSITIONS;
};

// Render order: back → front. Outerwear behind top; bottom behind shoes; accessories on top.
export const RENDER_ORDER: ItemZone[] = ['outerwear', 'bottoms', 'tops', 'shoes', 'accessories'];

// ---------- Role scaling ----------

// Multipliers applied to a zone's base width/height based on the generator's
// per-item layoutRole. The hero piece is amplified, accents are tucked smaller,
// supports stay at the existing size.
export const ROLE_SCALE: Record<'hero' | 'support' | 'accent', number> = {
  hero: 1.12,
  support: 1.0,
  accent: 0.82,
};

export const scalePos = (pos: ZonePos, factor: number): ZonePos => {
  const parsePct = (v: `${number}%`) => parseFloat(v.slice(0, -1));
  const w = parsePct(pos.w) * factor;
  const h = parsePct(pos.h) * factor;
  return {
    l: pos.l,
    t: pos.t,
    w: `${w}%`,
    h: `${h}%`,
  };
};

// ---------- Collage component ----------

export interface CollageProps {
  itemIds: string[];
  itemMap: Map<string, WardrobeItem>;
  snapshots?: OutfitItem[];
  layoutRoles?: Record<string, 'hero' | 'support' | 'accent'>;
  onItemPress?: (itemId: string) => void;
  colorScheme: 'light' | 'dark';
  /** Backend-resolved URL for the panel texture. Falls back to a bundled
   *  image when absent (offline dev, pre-surface-feature outfits). */
  panelUrl?: string;
  /** Backend-resolved URL for the ambient background. Same fallback rule. */
  backgroundUrl?: string;
  /** Per-archetype alignment scores for the outfit. Used by the local
   *  fallback picker to choose a panel + background that actually match
   *  the outfit's vibe instead of picking at random. Ignored when the
   *  backend already supplied panelUrl / backgroundUrl. */
  archetypeScores?: Record<string, number>;
  /** When true, the collage fills its parent's remaining vertical space via
   *  flex instead of its default 3:4 portrait aspect ratio. Use this when
   *  the collage is the only focal content on the screen (saved board view)
   *  so the image doesn't leave empty space below it. */
  fill?: boolean;
}

export const Collage: React.FC<CollageProps> = ({ itemIds, itemMap, snapshots, layoutRoles, onItemPress, colorScheme, panelUrl, backgroundUrl, archetypeScores, fill }) => {
  // Build a snapshot lookup for fallback when items have been deleted.
  const snapshotMap = useMemo(() => {
    const map = new Map<string, OutfitItem>();
    if (snapshots) {
      for (const s of snapshots) map.set(s.id, s);
    }
    return map;
  }, [snapshots]);

  // Pick local fallback panel + background per outfit identity. Memoized on
  // the item-id tuple + archetype fingerprint so the same card keeps the
  // same surface across re-renders, but different outfits get different
  // surfaces matched to their archetype mix. These are only used when the
  // server didn't supply panelUrl / backgroundUrl.
  const seed = itemIds.join('|');
  const panelSource = useMemo(
    () => pickSurface(LOCAL_PANELS, archetypeScores, seed),
    [seed, archetypeScores],
  );
  const backgroundSource = useMemo(
    () => pickSurface(LOCAL_BACKGROUNDS, archetypeScores, seed),
    [seed, archetypeScores],
  );

  const sorted = useMemo(() => {
    // First pass — classify each item's zone so we know the composition.
    const classified = itemIds.slice(0, 5).map(itemId => {
      const item = itemMap.get(itemId);
      const snapshot = snapshotMap.get(itemId);
      const category = item?.category ?? snapshot?.category ?? '';
      const zone: ItemZone = category ? classifyZone(category) : 'tops';
      return { itemId, item, snapshot, zone };
    });

    // Pick the layout table that matches which zones are actually present.
    // A 4-item look without outerwear reads left-heavy against the default
    // table; NO_OUTER_POSITIONS pulls everything toward center + diagonal.
    const activeZones = new Set(classified.map(c => c.zone));
    const positionsTable = pickLayout(activeZones);

    const zoneCounts = new Map<ItemZone, number>();
    const placed = classified.map(({ itemId, item, snapshot, zone }) => {
      const idx = zoneCounts.get(zone) ?? 0;
      zoneCounts.set(zone, idx + 1);
      const positions = positionsTable[zone];
      const basePos = positions[Math.min(idx, positions.length - 1)];
      // Apply the generator's layoutRole hint when present.
      const role = layoutRoles?.[itemId];
      const pos = role ? scalePos(basePos, ROLE_SCALE[role]) : basePos;
      return { itemId, item, snapshot, zone, pos };
    });
    return [...placed].sort(
      (a, b) => RENDER_ORDER.indexOf(a.zone) - RENDER_ORDER.indexOf(b.zone),
    );
  }, [itemIds, itemMap, snapshotMap, layoutRoles]);

  const collageBg = grays.gray6[colorScheme];
  const iconFallbackColor = labels.quaternary[colorScheme];

  return (
    <View style={[fill ? styles.collageFill : styles.collage, { backgroundColor: collageBg }]}>
      {/* Environment behind the panel. Fully opaque; the visible strip
          around the panel is the 3.5% inset applied below. The backend's
          LLM-chosen background URL takes priority; falls back to a bundled
          texture for offline dev or cache entries predating the feature. */}
      <Image
        source={backgroundUrl ? { uri: backgroundUrl } : backgroundSource}
        style={styles.collageBackground}
        contentFit="cover"
        cachePolicy="memory-disk"
      />
      {/* Textured panel the garments sit on. Inset from each edge so a thin
          frame of the background shows around it, and wrapped in a shadow
          container so the panel reads as a raised surface, not a sticker.
          Backend-chosen URL takes priority; falls back to the deterministic
          local panel pick when absent. */}
      <View style={[styles.collagePanelWrapper, PANEL_SHADOW_STYLE]}>
        <Image
          source={panelUrl ? { uri: panelUrl } : panelSource}
          style={styles.collagePanel}
          contentFit="cover"
          cachePolicy="memory-disk"
        />
      </View>
      {sorted.map(({ itemId, item, snapshot, pos }) => {
        const imgUrl = item?.pngImageUrl || item?.imageUrl
          || snapshot?.pngImageUrl || snapshot?.imageUrl;
        const content = imgUrl ? (
          <Image source={{ uri: imgUrl }} style={styles.collageItem} contentFit="contain" cachePolicy="memory-disk" />
        ) : (
          <Icon name="closet" size={32} color={iconFallbackColor} />
        );
        const itemStyle = {
          position: 'absolute' as const,
          left: pos.l,
          top: pos.t,
          width: pos.w,
          height: pos.h,
        };
        return onItemPress ? (
          <Pressable key={itemId} style={[itemStyle, ITEM_SHADOW_STYLE]} onPress={() => onItemPress(itemId)}>
            {content}
          </Pressable>
        ) : (
          <View key={itemId} style={[itemStyle, ITEM_SHADOW_STYLE]}>
            {content}
          </View>
        );
      })}
    </View>
  );
};

const styles = StyleSheet.create({
  // Collage — items absolutely positioned by clothing zone.
  // Sized relative to the card so phones and tablets get the same proportions.
  // flexShrink lets the collage yield height when the header grows (palette
  // strip, weather chip) so the bottom CTA never gets clipped.
  collage: {
    width: '100%',
    aspectRatio: 3 / 4, // portrait — width:height = 3:4
    alignSelf: 'center',
    borderRadius: 16,
    overflow: 'hidden',
    flexShrink: 1,
    minHeight: 0,
  },
  // Fill variant — grows to consume all remaining vertical space in a flex
  // column parent. Item positions are percentages so they scale with the
  // larger surface without layout changes.
  collageFill: {
    flex: 1,
    width: '100%',
    alignSelf: 'center',
    borderRadius: 16,
    overflow: 'hidden',
  },
  // Clothing item image — fills its positioned container.
  collageItem: {
    width: '100%',
    height: '100%',
  },
  // Background layer — the "environment" the panel sits in. Fills the
  // entire collage bounds so it can peek out around the inset panel.
  collageBackground: {
    position: 'absolute',
    left: 0,
    top: 0,
    width: '100%',
    height: '100%',
  },
  // Positioning + shadow host for the panel. Kept separate from the image so
  // the image can carry borderRadius (rounded corners) while the wrapper
  // casts an unclipped shadow onto the background.
  collagePanelWrapper: {
    position: 'absolute',
    left: '3.5%',
    top: '3.5%',
    width: '93%',
    height: '93%',
    borderRadius: 12,
  },
  // Panel texture itself — fills its shadow wrapper.
  collagePanel: {
    width: '100%',
    height: '100%',
    borderRadius: 12,
  },
});
