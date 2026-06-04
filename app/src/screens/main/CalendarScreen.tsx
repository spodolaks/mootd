import { Icon, Text } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, button, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { spacing } from '@/src/theme/spacing';
import { radius } from '@/src/theme/radius';
import React, { useState, useCallback, useMemo } from 'react';
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  View,
} from 'react-native';
import { Skeleton } from '@/src/components/ui';
import { Image } from 'expo-image';
import { Calendar, DateData } from 'react-native-calendars';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useFocusEffect } from '@react-navigation/native';
import { moodBoardRepository, wardrobeRepository } from '@/src/data/repositories';
import type { OutfitItem, SavedMoodBoard, WardrobeItem } from '@/src/domain';
import { getApiBaseURL } from '@/src/data/api/client';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';
import { useUIStore } from '@/src/store';
import { shareMoodboard, type SharePlatform } from '@/src/lib/shareMoodboard';

const toAbsoluteUrl = (url: string): string => {
  if (!url || url.startsWith('http')) return url;
  return `${getApiBaseURL()}${url}`;
};

export const CalendarScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const tabBottomPadding = useTabContentBottomPadding();
  const [selectedDate, setSelectedDate] = useState<string>(new Date().toISOString().split('T')[0]);
  const [boards, setBoards] = useState<SavedMoodBoard[]>([]);
  const [itemMap, setItemMap] = useState<Map<string, WardrobeItem>>(new Map());
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false); // mootd#50 — pull-to-refresh
  // #24 — track which platform a share is currently in-flight for so we
  // can disable both buttons + show a spinner, preventing double-tap
  // double-downloads on Instagram web.
  const [sharingTo, setSharingTo] = useState<SharePlatform | null>(null);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const tertiaryText = labels.tertiary[colorScheme];
  const cardBg = grays.gray5[colorScheme];
  const accentBg = button.primary.background[colorScheme];
  const accentText = button.primary.foreground[colorScheme];

  // Load moodboards + wardrobe items on focus
  useFocusEffect(
    useCallback(() => {
      let cancelled = false;
      const load = async () => {
        setIsLoading(true);
        try {
          // getAllItems for the same reason MoodBoardScreen does:
          // saved boards reference items by id, and the lookup
          // table has to span the whole wardrobe or the older
          // boards (saved before items were deleted/added past
          // page 1) render placeholders.
          const [boardsList, items] = await Promise.all([
            moodBoardRepository.list(),
            wardrobeRepository.getAllItems(),
          ]);
          if (cancelled) return;
          setBoards(boardsList);
          const map = new Map<string, WardrobeItem>();
          for (const item of items) map.set(item.id, item);
          setItemMap(map);
        } catch {
          // silently fail — calendar still usable
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

  // mootd#50 — pull-to-refresh handler. Same load steps as the
  // focus effect but keeps `isLoading` low so the existing
  // calendar grid stays put; the native pull spinner is the
  // only spinner shown.
  const onRefresh = useCallback(async () => {
    setIsRefreshing(true);
    try {
      const [boardsList, items] = await Promise.all([
        moodBoardRepository.list(),
        wardrobeRepository.getAllItems(),
      ]);
      setBoards(boardsList);
      const map = new Map<string, WardrobeItem>();
      for (const item of items) map.set(item.id, item);
      setItemMap(map);
    } catch {
      // silently fail — same UX as focus load.
    } finally {
      setIsRefreshing(false);
    }
  }, []);

  // Index boards by date for quick lookup
  const boardsByDate = useMemo(() => {
    const map = new Map<string, SavedMoodBoard>();
    // Keep only the latest board per date
    for (const b of [...boards].reverse()) {
      map.set(b.date, b);
    }
    return map;
  }, [boards]);

  const selectedBoard = boardsByDate.get(selectedDate) ?? null;

  const showToast = useUIStore(s => s.showToast);

  const handleShare = useCallback(
    async (platform: SharePlatform) => {
      if (!selectedBoard?.imageUrl) return;
      if (sharingTo !== null) return; // #24 — already in-flight; ignore tap
      setSharingTo(platform);
      try {
        const result = await shareMoodboard(platform, {
          imageUrl: toAbsoluteUrl(selectedBoard.imageUrl),
          caption: selectedBoard.outfit.name,
        });
        switch (result.kind) {
          case 'downloaded':
            showToast(result.message, 'info');
            break;
          case 'shared':
            // #24 — web returns a message so Facebook feels symmetric
            // with Instagram. Native's share sheet is self-evident, so
            // it leaves the message empty and we stay silent.
            if (result.message) showToast(result.message, 'info');
            break;
          case 'error':
            showToast("Couldn't open share sheet — try again.", 'error');
            break;
          // 'dismissed' leaves no feedback — the user cancelled intentionally.
        }
      } finally {
        setSharingTo(null);
      }
    },
    [selectedBoard, showToast, sharingTo]
  );

  // Build marked dates: dots for dates with outfits, highlight for selected
  const markedDates = useMemo(() => {
    const marks: Record<string, object> = {};
    for (const date of boardsByDate.keys()) {
      marks[date] = {
        marked: true,
        dotColor: accentBg,
      };
    }
    // Selected date overwrites
    marks[selectedDate] = {
      ...(marks[selectedDate] ?? {}),
      selected: true,
      selectedColor: accentBg,
      selectedTextColor: accentText,
    };
    return marks;
  }, [boardsByDate, selectedDate, accentBg, accentText]);

  const handleDayPress = (day: DateData) => {
    setSelectedDate(day.dateString);
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={styles.header}>
        <Text style={[styles.title, { color: textColor }]}>Calendar</Text>
      </View>

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[styles.scrollContent, { paddingBottom: tabBottomPadding }]}
        showsVerticalScrollIndicator={false}
        refreshControl={
          <RefreshControl
            refreshing={isRefreshing}
            onRefresh={() => {
              void onRefresh();
            }}
            tintColor={textColor}
          />
        }>
        {/* Calendar */}
        <View style={[styles.calendarContainer, { backgroundColor: cardBg }]}>
          <Calendar
            onDayPress={handleDayPress}
            markedDates={markedDates}
            theme={{
              backgroundColor: 'transparent',
              calendarBackground: 'transparent',
              textSectionTitleColor: tertiaryText,
              selectedDayBackgroundColor: accentBg,
              selectedDayTextColor: accentText,
              todayTextColor: textColor,
              dayTextColor: textColor,
              textDisabledColor: tertiaryText,
              monthTextColor: textColor,
              arrowColor: textColor,
              textMonthFontFamily: 'MontserratAlternates-SemiBold',
              textDayHeaderFontFamily: 'Inter-Regular',
              textDayFontFamily: 'Inter-Regular',
              textDayFontSize: 17,
              textMonthFontSize: 17,
              textDayHeaderFontSize: 13,
            }}
            style={styles.calendar}
          />
        </View>

        {/* Selected date outfit */}
        {isLoading ? (
          // mootd#50 — skeleton outfit card on initial load.
          // Same dimensions as the real card so there's no
          // jump when data arrives.
          <View style={[styles.outfitCard, { backgroundColor: cardBg }]}>
            <Skeleton style={styles.skeletonTitle} />
            <Skeleton style={styles.skeletonDesc} />
            <Skeleton style={styles.skeletonHero} />
          </View>
        ) : selectedBoard ? (
          <View style={[styles.outfitCard, { backgroundColor: cardBg }]}>
            <Text variant="headline" weight="semiBold" style={{ color: textColor }}>
              {selectedBoard.outfit.name}
            </Text>
            <Text
              variant="subheadline"
              style={[styles.outfitDescription, { color: secondaryText }]}>
              {selectedBoard.outfit.description}
            </Text>

            {/* Rendered moodboard hero — present only on saves made after the
                client-side capture feature shipped. Older rows fall through
                to the item-thumbnail row below, unchanged. */}
            {selectedBoard.imageUrl ? (
              <>
                <Image
                  source={{ uri: toAbsoluteUrl(selectedBoard.imageUrl) }}
                  style={styles.heroImage}
                  contentFit="cover"
                  cachePolicy="memory-disk"
                  accessibilityLabel="Saved moodboard collage"
                />
                {/* Share row — surfaces only when we actually have a render
                    to share. Legacy rows without a collage image keep the
                    existing thumbnail-only layout. */}
                <View style={styles.shareRow}>
                  <Text variant="footnote" style={{ color: tertiaryText }}>
                    Share
                  </Text>
                  {/* #24 — while a share is in-flight (sharingTo set),
                      disable BOTH buttons so a double-tap on Instagram
                      web doesn't trigger two downloads. Active button
                      shows a spinner in place of the icon so the user
                      has unambiguous feedback. */}
                  <Pressable
                    onPress={() => {
                      void handleShare('instagram');
                    }}
                    disabled={sharingTo !== null}
                    style={[
                      styles.shareBtn,
                      { backgroundColor: fills.tertiary[colorScheme] },
                      sharingTo !== null && { opacity: 0.5 },
                    ]}
                    hitSlop={8}
                    accessibilityRole="button"
                    accessibilityLabel="Share to Instagram"
                    accessibilityState={{
                      disabled: sharingTo !== null,
                      busy: sharingTo === 'instagram',
                    }}>
                    {sharingTo === 'instagram' ? (
                      <ActivityIndicator size="small" color={textColor} />
                    ) : (
                      <Icon name="instagram" size={20} color={textColor} />
                    )}
                  </Pressable>
                  <Pressable
                    onPress={() => {
                      void handleShare('facebook');
                    }}
                    disabled={sharingTo !== null}
                    style={[
                      styles.shareBtn,
                      { backgroundColor: fills.tertiary[colorScheme] },
                      sharingTo !== null && { opacity: 0.5 },
                    ]}
                    hitSlop={8}
                    accessibilityRole="button"
                    accessibilityLabel="Share to Facebook"
                    accessibilityState={{
                      disabled: sharingTo !== null,
                      busy: sharingTo === 'facebook',
                    }}>
                    {sharingTo === 'facebook' ? (
                      <ActivityIndicator size="small" color={textColor} />
                    ) : (
                      <Icon name="facebook" size={20} color={textColor} />
                    )}
                  </Pressable>
                </View>
              </>
            ) : null}

            {/* Item thumbnails */}
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={styles.itemsRow}>
              {selectedBoard.outfit.items.map(itemId => {
                const item = itemMap.get(itemId);
                const snapshot = (selectedBoard.outfit.snapshots ?? []).find(
                  (s: OutfitItem) => s.id === itemId
                );
                const imgUrl =
                  item?.pngImageUrl ||
                  item?.imageUrl ||
                  snapshot?.pngImageUrl ||
                  snapshot?.imageUrl;
                const label = item?.label ?? snapshot?.label;
                return (
                  <View
                    key={itemId}
                    style={[styles.itemThumb, { backgroundColor: fills.tertiary[colorScheme] }]}>
                    {imgUrl ? (
                      <Image
                        source={{ uri: toAbsoluteUrl(imgUrl) }}
                        style={styles.itemImage}
                        contentFit="contain"
                        cachePolicy="memory-disk"
                      />
                    ) : (
                      <Icon name="closet" size={24} color={tertiaryText} />
                    )}
                    {label && (
                      <Text
                        variant="caption2"
                        numberOfLines={1}
                        style={[styles.itemLabel, { color: secondaryText }]}>
                        {label}
                      </Text>
                    )}
                  </View>
                );
              })}
            </ScrollView>

            {/* Suggestions */}
            {selectedBoard.outfit.suggestions && selectedBoard.outfit.suggestions.length > 0 && (
              <View style={styles.suggestionsContainer}>
                <Text variant="footnote" weight="semiBold" style={{ color: tertiaryText }}>
                  Suggestions
                </Text>
                {selectedBoard.outfit.suggestions.map((s, i) => (
                  <Text key={i} variant="footnote" style={{ color: tertiaryText }}>
                    {'\u2022'} {s}
                  </Text>
                ))}
              </View>
            )}
          </View>
        ) : (
          <View style={styles.emptyDetail}>
            <Icon name="calendar" size={32} color={tertiaryText} />
            <Text variant="subheadline" style={[styles.emptyText, { color: tertiaryText }]}>
              No outfit saved for this date
            </Text>
          </View>
        )}
      </ScrollView>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  title: {
    ...typography.largeTitle.semiBold,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingHorizontal: spacing.lg,
    paddingBottom: 40,
    gap: spacing.md,
  },
  calendarContainer: {
    borderRadius: radius.xl,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.sm,
  },
  calendar: {
    borderRadius: radius.xl,
  },
  detailLoading: {
    paddingVertical: 40,
    alignItems: 'center',
  },
  outfitCard: {
    borderRadius: radius.xl,
    padding: spacing.md,
    gap: spacing.sm,
  },
  // mootd#50 — skeleton outfit-card placeholders.
  skeletonTitle: {
    height: 22,
    width: '60%',
    borderRadius: 4,
  },
  skeletonDesc: {
    height: 14,
    width: '90%',
    borderRadius: 4,
  },
  skeletonHero: {
    width: '100%',
    aspectRatio: 1,
    borderRadius: radius.lg,
    marginTop: spacing.sm,
  },
  outfitDescription: {
    lineHeight: 20,
  },
  heroImage: {
    width: '100%',
    aspectRatio: 1,
    borderRadius: radius.lg,
    marginTop: spacing.sm,
  },
  shareRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    marginTop: spacing.sm,
  },
  shareBtn: {
    width: 36,
    height: 36,
    borderRadius: radius.full,
    justifyContent: 'center',
    alignItems: 'center',
  },
  itemsRow: {
    gap: spacing.sm,
    paddingVertical: spacing.sm,
  },
  itemThumb: {
    width: 80,
    height: 100,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },
  itemImage: {
    width: '100%',
    height: 72,
  },
  itemLabel: {
    marginTop: 4,
    paddingHorizontal: 4,
    textAlign: 'center',
  },
  suggestionsContainer: {
    gap: 4,
    marginTop: spacing.xs,
  },
  emptyDetail: {
    alignItems: 'center',
    paddingVertical: 40,
    gap: spacing.sm,
  },
  emptyText: {
    textAlign: 'center',
  },
});
