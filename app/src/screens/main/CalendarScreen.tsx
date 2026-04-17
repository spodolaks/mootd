import { Icon, Text } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, button, fills, grays, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { spacing } from '@/src/theme/spacing';
import { radius } from '@/src/theme/radius';
import React, { useState, useCallback, useMemo } from 'react';
import { ActivityIndicator, ScrollView, StyleSheet, View } from 'react-native';
import { Image } from 'expo-image';
import { Calendar, DateData } from 'react-native-calendars';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useFocusEffect } from '@react-navigation/native';
import { moodBoardRepository, wardrobeRepository } from '@/src/data/repositories';
import type { OutfitItem, SavedMoodBoard, WardrobeItem } from '@/src/domain';
import { getApiBaseURL } from '@/src/data/api/client';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';

const toAbsoluteUrl = (url: string): string => {
  if (!url || url.startsWith('http')) return url;
  return `${getApiBaseURL()}${url}`;
};

export const CalendarScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const tabBottomPadding = useTabContentBottomPadding();
  const [selectedDate, setSelectedDate] = useState<string>(
    new Date().toISOString().split('T')[0],
  );
  const [boards, setBoards] = useState<SavedMoodBoard[]>([]);
  const [itemMap, setItemMap] = useState<Map<string, WardrobeItem>>(new Map());
  const [isLoading, setIsLoading] = useState(true);

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
          const [boardsList, { items }] = await Promise.all([
            moodBoardRepository.list(),
            wardrobeRepository.getItems(),
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
      return () => { cancelled = true; };
    }, []),
  );

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
      >
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
          <View style={styles.detailLoading}>
            <ActivityIndicator size="small" color={textColor} />
          </View>
        ) : selectedBoard ? (
          <View style={[styles.outfitCard, { backgroundColor: cardBg }]}>
            <Text variant="headline" weight="semiBold" style={{ color: textColor }}>
              {selectedBoard.outfit.name}
            </Text>
            <Text variant="subheadline" style={[styles.outfitDescription, { color: secondaryText }]}>
              {selectedBoard.outfit.description}
            </Text>

            {/* Item thumbnails */}
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              contentContainerStyle={styles.itemsRow}
            >
              {selectedBoard.outfit.items.map((itemId) => {
                const item = itemMap.get(itemId);
                const snapshot = (selectedBoard.outfit.snapshots ?? []).find(
                  (s: OutfitItem) => s.id === itemId,
                );
                const imgUrl = item?.pngImageUrl || item?.imageUrl
                  || snapshot?.pngImageUrl || snapshot?.imageUrl;
                const label = item?.label ?? snapshot?.label;
                return (
                  <View
                    key={itemId}
                    style={[styles.itemThumb, { backgroundColor: fills.tertiary[colorScheme] }]}
                  >
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
                        style={[styles.itemLabel, { color: secondaryText }]}
                      >
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
  outfitDescription: {
    lineHeight: 20,
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
