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

// TRIANGULAR_POSITIONS — P1-E editorial diagonal layout (default when
// outerwear is present). Items flow along a top-right → bottom-left
// diagonal with the outerwear anchor at the upper-right third, the top
// at mid-left, bottoms center-lower, shoes baseline-left, and accessories
// tucked adjacent to the outerwear anchor. This mirrors the composition
// stylists use in editorial flat-lays (Who What Wear, The Zoe Report,
// top Pinterest outfit pins) — a deliberate zigzag that the eye traces
// naturally, instead of the retail-style vertical stack.
//
// Each zone's index-0 position is tuned so its center aligns with (or
// sits adjacent to) the matching HERO_THIRDS_CENTER intersection — so
// when the item IS the hero, the P0 thirds-repositioning produces a
// subtle shift rather than a jarring jump.
export const TRIANGULAR_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  outerwear: [
    { l: '38%', t: '13%', w: '55%', h: '42%' },
    { l: '34%', t: '15%', w: '52%', h: '40%' },
    { l: '30%', t: '17%', w: '48%', h: '38%' },
  ],
  tops: [
    { l: '6%',  t: '28%', w: '48%', h: '38%' },
    { l: '4%',  t: '30%', w: '46%', h: '36%' },
    { l: '2%',  t: '32%', w: '42%', h: '34%' },
  ],
  bottoms: [
    { l: '28%', t: '54%', w: '50%', h: '40%' },
    { l: '26%', t: '56%', w: '46%', h: '38%' },
    { l: '24%', t: '58%', w: '42%', h: '36%' },
  ],
  shoes: [
    { l: '6%',  t: '78%', w: '28%', h: '18%' },
    { l: '6%',  t: '80%', w: '26%', h: '16%' },
    { l: '6%',  t: '82%', w: '24%', h: '14%' },
  ],
  // P1-G: accessories clustered adjacent to the typical outerwear anchor
  // (top-right third) rather than isolated in a corner. When the outfit's
  // hero is elsewhere, the accessory still reads as a counter-balance
  // rather than an orphaned element.
  accessories: [
    { l: '72%', t: '58%', w: '22%', h: '18%' },
    { l: '74%', t: '62%', w: '20%', h: '16%' },
    { l: '76%', t: '66%', w: '18%', h: '14%' },
  ],
};

// TRIANGULAR_MIRRORED_POSITIONS — P2-K visual-variety sibling of
// TRIANGULAR_POSITIONS. Mirrors the default: outerwear anchors at the
// top-LEFT third, tops at mid-right, bottoms center-lower-right, shoes
// bottom-right, accessories tucked near the top-left outerwear. The eye
// traces the opposite diagonal (top-left → bottom-right).
//
// Why mirror specifically: editorial flat-lays don't always run
// right-anchored; anchor-left is equally valid and common in Scandinavian
// / minimalist editorial work. Offering both and seed-selecting between
// them means two consecutive outfits with the same item set don't look
// identical — the single biggest "feels static" complaint we've heard.
export const TRIANGULAR_MIRRORED_POSITIONS: Record<ItemZone, [ZonePos, ZonePos, ZonePos]> = {
  outerwear: [
    { l: '7%',  t: '13%', w: '55%', h: '42%' },
    { l: '9%',  t: '15%', w: '52%', h: '40%' },
    { l: '11%', t: '17%', w: '48%', h: '38%' },
  ],
  tops: [
    { l: '46%', t: '28%', w: '48%', h: '38%' },
    { l: '48%', t: '30%', w: '46%', h: '36%' },
    { l: '50%', t: '32%', w: '42%', h: '34%' },
  ],
  bottoms: [
    { l: '22%', t: '54%', w: '50%', h: '40%' },
    { l: '24%', t: '56%', w: '46%', h: '38%' },
    { l: '26%', t: '58%', w: '42%', h: '36%' },
  ],
  shoes: [
    { l: '66%', t: '78%', w: '28%', h: '18%' },
    { l: '68%', t: '80%', w: '26%', h: '16%' },
    { l: '70%', t: '82%', w: '24%', h: '14%' },
  ],
  accessories: [
    { l: '6%',  t: '58%', w: '22%', h: '18%' },
    { l: '6%',  t: '62%', w: '20%', h: '16%' },
    { l: '6%',  t: '66%', w: '18%', h: '14%' },
  ],
};

// Legacy export for any callers still referencing the old name.
// Now points at the triangular default so legacy consumers see the new
// composition without code changes.
export const ZONE_POSITIONS = TRIANGULAR_POSITIONS;

// pickLayout returns the positions table best suited to the set of zones
// actually present on this card. Triangular is the editorial default when
// outerwear anchors the look (with or without accessory). P2-K adds a
// seed-based 50/50 pick between TRIANGULAR and TRIANGULAR_MIRRORED so
// consecutive outfits with identical zone sets don't render identically.
// When outerwear is missing the diagonal breaks down, so we fall back to
// the older NO_OUTER layout. Minimal (3-zone) stays as the legacy outfit
// fallback.
export const pickLayout = (
  activeZones: Set<ItemZone>,
  seed?: string,
): Record<ItemZone, [ZonePos, ZonePos, ZonePos]> => {
  const hasOuter = activeZones.has('outerwear');
  const hasAccessory = activeZones.has('accessories');
  if (hasOuter) {
    // P2-K: flip between the two triangular variants per outfit so two
    // back-to-back boards with the same zones look distinct. Missing
    // seed → default variant (stable behaviour for test/story fixtures).
    if (!seed) return TRIANGULAR_POSITIONS;
    return (hashSeed(seed) & 1) === 0
      ? TRIANGULAR_POSITIONS
      : TRIANGULAR_MIRRORED_POSITIONS;
  }
  if (hasAccessory) return NO_OUTER_POSITIONS;
  return MINIMAL_POSITIONS;
};

// Render order: back → front. Outerwear behind top; bottom behind shoes; accessories on top.
export const RENDER_ORDER: ItemZone[] = ['outerwear', 'bottoms', 'tops', 'shoes', 'accessories'];

// ---------- Editorial sizing (P0-A, P0-B, P0-C, P0-D) ----------
//
// The composition engine derives each item's final bounding box from three
// factors: a per-zone weight (anchor vs tucked), a per-role weight (hero vs
// support vs accent), and a reflow boost when fewer than 4 zones are active.
// Hero items additionally reposition onto a rule-of-thirds intersection for
// triangular/diagonal editorial composition.
//
// Design principles synthesized from pro flat-lay research (Who What Wear,
// The Zoe Report, FIT/Parsons styling coursework, top Pinterest outfit
// pins): hero 2–3× the area of accent, outerwear as anchor, shoes rendered
// 60–75% of proportional (the #1 amateur flat-lay tell), accessories
// supporting not competing, consistent 8–10% safe margin and 2–3% gutter.

// Safe margin inside the canvas — no item's bounding box should extend into
// the outer SAFE_MARGIN band. Currently enforced by the panel inset (3.5%)
// plus per-zone l/t defaults; this constant is the reference for future
// layout work and for the hero-thirds clamp below.
export const SAFE_MARGIN = 0.09;

// Consistent gutter between adjacent items. Stylist-taught target 2–3% of
// canvas width. Codified for future layout work (triangular P1-E).
export const ITEM_GUTTER = 0.025;

// Per-zone base weight applied on top of role scaling. Outerwear gets a
// small anchor boost (editorial convention). Shoes are explicitly reduced —
// rendering footwear at realistic proportion is the #1 amateur flat-lay
// mistake; pros render shoes 60–75% of what proportional math gives you.
// Accessories are slightly tucked (the cluster should read as supporting,
// not competing).
export const ZONE_WEIGHT: Record<ItemZone, number> = {
  outerwear:   1.05,
  tops:        1.00,
  bottoms:     1.00,
  shoes:       0.85,
  accessories: 0.92,
};

// Replaces the old flat ROLE_SCALE. Hero is stronger (1.15 vs 1.12) and
// accent is more tucked (0.76 vs 0.82) for clearer visual hierarchy.
// Combined with ZONE_WEIGHT, a hero outerwear is ~2.3× the area of an
// accent accessory — in the 2–3× "editorial" range stylists recommend.
export const ROLE_SCALE: Record<'hero' | 'support' | 'accent', number> = {
  hero: 1.15,
  support: 1.0,
  accent: 0.76,
};

// When fewer than 4 zones are active on the canvas (legacy outfit missing
// an item, 3-zone minimal layout), remaining items absorb ~8% of the
// would-be missing zone's space so the panel doesn't read as "half
// empty". New outfits always have 4+ zones (backend validation enforces
// top + bottom + shoes minimum at `service.go:374`), so this mainly
// benefits saved boards predating validation.
export const REFLOW_FACTOR_PER_MISSING = 1.08;

// P1-H visual-weight multiplier. The LLM marks the ONE signature piece
// per outfit with "statement"; that item renders ~1.35× its base size
// (area ratio ~1.8×) so a statement bag reads as the outfit's identity
// even when its layoutRole is "accent". "minor" tucks an item further
// back (plain watch in a jacket-led outfit). Most items carry either
// "supporting" or no mark and render at their role's default size.
export const VISUAL_WEIGHT_SCALE: Record<'statement' | 'supporting' | 'minor', number> = {
  statement: 1.35,
  supporting: 1.0,
  minor: 0.82,
};

// Rule-of-thirds centers per zone. When an item has role='hero', its
// position is computed from these centers instead of the zone table —
// placing the hero on a third-line intersection for editorial
// (triangular/diagonal) composition rather than a dead-center retail
// stack. Support and accent items use the zone table unchanged so the
// overall layout stays recognizable.
//
// Each zone targets a different intersection so boards with different
// hero zones feel visually distinct. Outerwear → top-right third; tops
// → top-left; bottoms → center-bottom; shoes + accessories keep zone-
// sensible positions (they're rarely hero; when they are, the outfit
// is built around them and the position feels intentional).
export const HERO_THIRDS_CENTER: Record<ItemZone, { cx: number; cy: number }> = {
  outerwear:   { cx: 65, cy: 34 },
  tops:        { cx: 35, cy: 32 },
  bottoms:     { cx: 50, cy: 65 },
  shoes:       { cx: 33, cy: 82 },
  accessories: { cx: 78, cy: 30 },
};

// scalePos — one-stop shop for deriving the final bounding box from a
// zone/role/visualWeight/activeZoneCount tuple. For hero items it
// repositions to a rule-of-thirds intersection; for support/accent it
// keeps the zone table's default anchor and just scales w/h.
export const scalePos = (
  basePos: ZonePos,
  zone: ItemZone,
  role: 'hero' | 'support' | 'accent' | undefined,
  visualWeight: 'statement' | 'supporting' | 'minor' | undefined,
  activeZoneCount: number,
): ZonePos => {
  const parsePct = (v: `${number}%`) => parseFloat(v.slice(0, -1));
  const zoneW = ZONE_WEIGHT[zone];
  const roleW = role ? ROLE_SCALE[role] : 1.0;
  const weightW = visualWeight ? VISUAL_WEIGHT_SCALE[visualWeight] : 1.0;
  // Reflow kicks in only when fewer than 4 zones are active, matching the
  // threshold where the MINIMAL_POSITIONS table takes over.
  const reflow = activeZoneCount < 4 ? REFLOW_FACTOR_PER_MISSING : 1.0;
  const factor = zoneW * roleW * weightW * reflow;
  const w = parsePct(basePos.w) * factor;
  const h = parsePct(basePos.h) * factor;

  if (role === 'hero') {
    // Re-center on the zone's thirds intersection. Clamp within the
    // panel bounds (3.5% – 96.5%) so the item doesn't overflow the
    // textured surface onto the visible background strip.
    const { cx, cy } = HERO_THIRDS_CENTER[zone];
    const l = Math.max(3.5, Math.min(96.5 - w, cx - w / 2));
    const t = Math.max(3.5, Math.min(96.5 - h, cy - h / 2));
    return { l: `${l}%`, t: `${t}%`, w: `${w}%`, h: `${h}%` };
  }

  // Support + accent: keep the zone's default anchor, just scale w/h.
  return {
    l: basePos.l,
    t: basePos.t,
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
  /** Per-item visual weight from the LLM (P1-H). Boosts the signature
   *  piece above its role's default size so a statement bag reads larger
   *  than a plain belt even when both carry role='accent'. Missing → all
   *  items scale purely by layoutRoles + zone weight. */
  visualWeights?: Record<string, 'statement' | 'supporting' | 'minor'>;
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

export const Collage: React.FC<CollageProps> = ({ itemIds, itemMap, snapshots, layoutRoles, visualWeights, onItemPress, colorScheme, panelUrl, backgroundUrl, archetypeScores, fill }) => {
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
    // Seed threads through so P2-K (pickLayout variety) can deterministic-
    // ally pick between TRIANGULAR and TRIANGULAR_MIRRORED per outfit.
    const activeZones = new Set(classified.map(c => c.zone));
    const positionsTable = pickLayout(activeZones, seed);
    // Reflow signal: when the outfit is missing a zone (legacy saved
    // board, or a minimal 3-zone composition), remaining items scale up
    // by REFLOW_FACTOR_PER_MISSING so the panel doesn't look half-empty.
    const activeZoneCount = activeZones.size;

    const zoneCounts = new Map<ItemZone, number>();
    const placed = classified.map(({ itemId, item, snapshot, zone }) => {
      const idx = zoneCounts.get(zone) ?? 0;
      zoneCounts.set(zone, idx + 1);
      const positions = positionsTable[zone];
      const basePos = positions[Math.min(idx, positions.length - 1)];
      // Compose zone weight × role weight × visual weight × reflow in one
      // pass. For hero items scalePos re-anchors onto a rule-of-thirds
      // intersection for editorial/triangular composition; support + accent
      // keep the zone table's default anchor. Visual weight (P1-H) boosts
      // the LLM-marked signature piece above its role's default size so a
      // statement bag reads larger than a plain belt even when both are
      // role='accent'.
      const role = layoutRoles?.[itemId];
      const weight = visualWeights?.[itemId];
      const pos = scalePos(basePos, zone, role, weight, activeZoneCount);
      return { itemId, item, snapshot, zone, pos };
    });

    // P1-G: cluster accessories adjacent to the hero.
    //
    // The zone table places accessories at a fixed top-right-ish anchor,
    // which reads well when outerwear is the hero (the common case) but
    // leaves the accessory orphaned when tops or bottoms carries the
    // hero role. Stylists explicitly cluster accessories next to the
    // outfit's center of gravity — never scattered to a corner.
    //
    // Strategy: find the hero's final bounding box, then override each
    // accessory's position to sit at the hero's lower-right quadrant,
    // offset by ITEM_GUTTER. Multiple accessories stack downward with
    // slight overlap so they read as a cluster, not a column.
    const heroPlaced = placed.find(p => layoutRoles?.[p.itemId] === 'hero');
    if (heroPlaced) {
      const pct = (v: `${number}%`) => parseFloat(v.slice(0, -1));
      const heroL = pct(heroPlaced.pos.l);
      const heroT = pct(heroPlaced.pos.t);
      const heroW = pct(heroPlaced.pos.w);
      const heroH = pct(heroPlaced.pos.h);
      const gutterPct = ITEM_GUTTER * 100;
      // Cluster anchor — slightly inside the hero's right edge so the
      // accessory visually "leans on" the hero rather than floating.
      const anchorX = heroL + heroW - heroW * 0.15;
      const anchorY = heroT + heroH * 0.55;

      let accessoryIdx = 0;
      placed.forEach(p => {
        if (p.zone !== 'accessories') return;
        const accW = pct(p.pos.w);
        const accH = pct(p.pos.h);
        // Stack subsequent accessories with 30% overlap so they read as
        // a tight cluster, not a vertical column.
        const verticalOffset = accessoryIdx * (accH * 0.7 + gutterPct);
        const targetL = anchorX;
        const targetT = anchorY + verticalOffset;
        // Clamp inside the panel's 3.5–96.5% safe band so the cluster
        // doesn't overflow onto the background strip.
        const l = Math.max(3.5, Math.min(96.5 - accW, targetL));
        const t = Math.max(3.5, Math.min(96.5 - accH, targetT));
        p.pos = {
          l: `${l}%`,
          t: `${t}%`,
          w: p.pos.w,
          h: p.pos.h,
        };
        accessoryIdx += 1;
      });
    }

    const ordered = [...placed].sort(
      (a, b) => RENDER_ORDER.indexOf(a.zone) - RENDER_ORDER.indexOf(b.zone),
    );

    // P1-F: seeded rotation on 1–2 non-hero items.
    //
    // Editorial flat-lays never render everything axis-aligned — the
    // slight tilt on a supporting garment is the single biggest "this
    // looks like a Pinterest pin, not a catalog page" signal. Pick 1 or
    // 2 non-hero items and tilt them 5–15°, seeded by the outfit's item
    // tuple so the same outfit always tilts the same items the same way
    // (no flicker across re-mounts, but every outfit looks different).
    //
    // Hero is explicitly excluded — the anchor must stay orthogonal so
    // the rest of the composition reads as "tilted around the hero".
    // Shoes are also excluded (rotated footwear looks like a mistake,
    // not a style choice).
    const hash = hashSeed(seed);
    const rotatable: number[] = [];
    ordered.forEach((p, i) => {
      const isHero = layoutRoles?.[p.itemId] === 'hero';
      const isShoes = p.zone === 'shoes';
      if (!isHero && !isShoes) rotatable.push(i);
    });
    const rotationByIndex = new Map<number, number>();
    if (rotatable.length > 0) {
      const count = Math.min(1 + (hash % 2), rotatable.length);
      for (let i = 0; i < count; i++) {
        // Stride by 13 to spread picks across the rotatable list even
        // when count === 2 (avoids rotating two adjacent items).
        const pickPos = (hash + i * 13) % rotatable.length;
        const targetIdx = rotatable[pickPos];
        if (rotationByIndex.has(targetIdx)) continue;
        const magnitude = 5 + ((hash + i * 7) % 11); // 5–15°
        const sign = ((hash + i) % 2 === 0) ? 1 : -1;
        rotationByIndex.set(targetIdx, magnitude * sign);
      }
    }

    return ordered.map((p, i) => ({
      ...p,
      rotation: rotationByIndex.get(i) ?? 0,
    }));
  }, [itemIds, itemMap, snapshotMap, layoutRoles, visualWeights, seed]);

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
      {sorted.map(({ itemId, item, snapshot, pos, rotation }) => {
        const imgUrl = item?.pngImageUrl || item?.imageUrl
          || snapshot?.pngImageUrl || snapshot?.imageUrl;
        const content = imgUrl ? (
          <Image source={{ uri: imgUrl }} style={styles.collageItem} contentFit="contain" cachePolicy="memory-disk" />
        ) : (
          <Icon name="closet" size={32} color={iconFallbackColor} />
        );
        // P1-F rotation: transform only when non-zero so the common
        // axis-aligned path stays allocation-free and the shadow bbox
        // isn't computed unnecessarily on iOS.
        const itemStyle = {
          position: 'absolute' as const,
          left: pos.l,
          top: pos.t,
          width: pos.w,
          height: pos.h,
          ...(rotation !== 0 ? { transform: [{ rotate: `${rotation}deg` }] } : {}),
        };
        // #22 — screen readers couldn't identify which garment they were
        // about to interact with; the Pressable had no accessibilityLabel
        // so VoiceOver / TalkBack announced only "button". Pull the label
        // from the live item first, falling back to the snapshot (for
        // saved boards whose wardrobe item was deleted) and finally a
        // generic "Swap this item" so blind users at least know it's
        // interactive rather than an ornamental image.
        const a11yLabel = item?.label ?? snapshot?.label ?? 'Swap this item';
        return onItemPress ? (
          <Pressable
            key={itemId}
            style={[itemStyle, ITEM_SHADOW_STYLE]}
            onPress={() => onItemPress(itemId)}
            accessibilityRole="button"
            accessibilityLabel={a11yLabel}
            accessibilityHint="Double tap to swap this garment"
          >
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
