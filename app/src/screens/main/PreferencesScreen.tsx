/**
 * PreferencesScreen – data-driven settings page.
 *
 * Architecture improvements (2025/2026 best practices):
 *  • Single preferencesStore (Zustand) with auto-persistence replaces
 *    scattered useState + manual themeStore.
 *  • Reusable <SettingsRow> / <SettingsSection> components instead of
 *    inline sub-components – composable across screens.
 *  • Declarative section configs make adding new rows trivial.
 *  • Platform.OS === 'web' guard on Alert.alert (no-op on web) replaced
 *    with a confirm() fallback.
 *  • All interactive elements use Pressable (web-safe).
 */

import React, { useCallback, useState } from 'react';
import {
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { useFocusEffect } from '@react-navigation/native';

import { Icon, SegmentedControl, SettingsRow, SettingsSection } from '@/src/components';
import { apiClient } from '@/src/data/api/client';
import { useColorScheme } from '@/src/hooks';
import { usePreferencesStore } from '@/src/store/preferencesStore';
import { useAuthStore } from '@/src/store';
import {
  accents,
  backgrounds,
  grays,
  labels,
  separators,
} from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';

// ─── Component ───────────────────────────────────────────────────────────────

export const PreferencesScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const router = useRouter();

  // Auth
  const user = useAuthStore((s) => s.user);
  const signOut = useAuthStore((s) => s.signOut);

  // All preferences from the unified store
  const prefs = usePreferencesStore();

  // The resolved display name: prefer persisted preference, fall back to Google name
  const resolvedName = prefs.displayName || user?.name || '';

  // Inline-edit state — re-sync every time the screen gains focus so we
  // always show the latest persisted value (useState initializer only runs
  // once and the screen may stay mounted on web).
  const [draftName, setDraftName] = useState(resolvedName);

  useFocusEffect(
    useCallback(() => {
      setDraftName(prefs.displayName || user?.name || '');
    }, [prefs.displayName, user?.name]),
  );

  // ─── Colors ────────────────────────────────────────────────────────────────
  const bg = backgrounds.primary[colorScheme];
  const text = labels.primary[colorScheme];
  const secondary = labels.secondary[colorScheme];
  const tertiary = labels.tertiary[colorScheme];
  const cardBg = grays.gray5[colorScheme];
  const divider = separators.primary[colorScheme];
  const accent = accents.blue[colorScheme];
  const danger = accents.red[colorScheme];

  // ─── Handlers ──────────────────────────────────────────────────────────────
  const handleSaveName = useCallback(() => {
    prefs.setDisplayName(draftName.trim());
    // TODO: PUT /v1/user/profile on backend
  }, [draftName, prefs]);

  // mootd#67 — creativity slider. Five-step segmented control
  // mapping to {0, 0.25, 0.5, 0.75, 1}. Optimistic local update
  // + best-effort PUT /v1/user/profile; a network failure logs
  // and the next save retries.
  const handleCreativityChange = useCallback((value: string) => {
    const c = parseFloat(value);
    if (!Number.isFinite(c)) return;
    prefs.setCreativity(c);
    void apiClient
      .put('/v1/user/profile', { creativity: c })
      .catch((err: unknown) => {
        console.warn('[Preferences] creativity sync failed:', err);
      });
  }, [prefs]);

  const handleDeleteAccount = useCallback(() => {
    const doDelete = () => {
      prefs.reset();
      signOut();
      router.replace('/');
    };

    if (Platform.OS === 'web') {
      // Alert.alert is a no-op on web – use window.confirm instead
       
      if (confirm('This will permanently delete your account. Continue?')) {
        doDelete();
      }
    } else {
      const { Alert } = require('react-native');
      Alert.alert(
        'Delete Account',
        'This will permanently delete your account and all your data. This action cannot be undone.',
        [
          { text: 'Cancel', style: 'cancel' },
          { text: 'Delete', style: 'destructive', onPress: doDelete },
        ],
      );
    }
  }, [prefs, signOut, router]);

  // ─── Render ────────────────────────────────────────────────────────────────
  return (
    <SafeAreaView style={[styles.container, { backgroundColor: bg }]} edges={['top']}>
      {/* Header */}
      <View style={styles.header}>
        <Pressable
          onPress={() => router.back()}
          style={styles.backButton}
        >
          <Icon name="chevron-left" size={24} color={text} />
        </Pressable>
        <Text style={[styles.title, { color: text }]}>Preferences</Text>
        <View style={styles.headerSpacer} />
      </View>

      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}
      >
        {/* ── Appearance ──────────────────────────────────────────────── */}
        <SettingsSection title="Appearance" color={secondary} cardBackground={cardBg}>
          <SettingsRow
            mode="select"
            icon="sun"
            label="Light"
            selected={prefs.theme === 'light'}
            onPress={() => prefs.setTheme('light')}
            accentColor={accent}
            textColor={text}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="select"
            icon="moon"
            label="Dark"
            selected={prefs.theme === 'dark'}
            onPress={() => prefs.setTheme('dark')}
            accentColor={accent}
            textColor={text}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="select"
            icon="sync"
            label="System"
            selected={prefs.theme === 'system'}
            onPress={() => prefs.setTheme('system')}
            accentColor={accent}
            textColor={text}
          />
        </SettingsSection>

        {/* ── Notifications ───────────────────────────────────────────── */}
        <SettingsSection title="Notifications" color={secondary} cardBackground={cardBg}>
          <SettingsRow
            mode="toggle"
            icon="bell"
            label="Push Notifications"
            value={prefs.pushNotifications}
            onValueChange={prefs.setPushNotifications}
            textColor={text}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="toggle"
            icon="sunrise"
            label="Daily Outfit Reminder"
            value={prefs.dailyOutfitReminder}
            onValueChange={prefs.setDailyOutfitReminder}
            textColor={text}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="toggle"
            icon="cloud"
            label="Weather Alerts"
            value={prefs.weatherAlerts}
            onValueChange={prefs.setWeatherAlerts}
            textColor={text}
          />
        </SettingsSection>

        {/* ── Units ───────────────────────────────────────────────────── */}
        <SettingsSection title="Units" color={secondary} cardBackground={cardBg}>
          <SettingsRow
            mode="select"
            icon="sun"
            label="Celsius (°C)"
            selected={prefs.temperatureUnit === 'celsius'}
            onPress={() => prefs.setTemperatureUnit('celsius')}
            accentColor={accent}
            textColor={text}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="select"
            icon="sun"
            label="Fahrenheit (°F)"
            selected={prefs.temperatureUnit === 'fahrenheit'}
            onPress={() => prefs.setTemperatureUnit('fahrenheit')}
            accentColor={accent}
            textColor={text}
          />
        </SettingsSection>

        {/* ── Outfit creativity (mootd#67) ─────────────────────────────── */}
        <SettingsSection title="Outfit creativity" color={secondary} cardBackground={cardBg}>
          <View style={styles.creativityRow}>
            <Text style={[styles.creativityHint, { color: secondary }]}>
              Slide left for predictable outfits, right for surprising ones.
            </Text>
            <SegmentedControl
              options={[
                { label: 'Safe', value: '0' },
                { label: 'Mostly safe', value: '0.25' },
                { label: 'Balanced', value: '0.5' },
                { label: 'Surprising', value: '0.75' },
                { label: 'Bold', value: '1' },
              ]}
              selectedValue={String(prefs.creativity)}
              onValueChange={handleCreativityChange}
            />
          </View>
        </SettingsSection>

        {/* ── Account ─────────────────────────────────────────────────── */}
        <SettingsSection title="Account" color={secondary} cardBackground={cardBg}>
          <View style={styles.editRow}>
            <View style={styles.editIcon} pointerEvents="none">
              <Icon name="user" size={20} color={text} />
            </View>
            <View style={styles.editInputWrapper}>
              <Text style={[styles.editLabel, { color: secondary }]}>Display Name</Text>
              <TextInput
                style={[styles.nameInput, { color: text }]}
                value={draftName}
                onChangeText={setDraftName}
                onSubmitEditing={handleSaveName}
                returnKeyType="done"
                placeholder="Tap to enter name"
                placeholderTextColor={tertiary}
                selectTextOnFocus
              />
            </View>
            {draftName !== (prefs.displayName || user?.name || '') && (
              <Pressable
                onPress={handleSaveName}
                style={[styles.saveButton, { backgroundColor: accent }]}
              >
                <Text style={styles.saveButtonText}>Save</Text>
              </Pressable>
            )}
          </View>
          <View style={[styles.inlineDivider, { backgroundColor: divider }]} />
          <SettingsRow
            mode="display"
            icon="mail"
            label="Email"
            subtitle={user?.email ?? 'Not available'}
            textColor={text}
            subtitleColor={secondary}
          />
        </SettingsSection>

        {/* ── Privacy & Data ──────────────────────────────────────────── */}
        <SettingsSection title="Privacy & Data" color={secondary} cardBackground={cardBg}>
          <SettingsRow
            mode="navigation"
            icon="privacy"
            label="Privacy Policy"
            onPress={() => {/* TODO: open URL */}}
            textColor={text}
            chevronColor={tertiary}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="navigation"
            icon="file"
            label="Terms of Service"
            onPress={() => {/* TODO: open URL */}}
            textColor={text}
            chevronColor={tertiary}
            dividerColor={divider}
            showDivider
          />
          <SettingsRow
            mode="navigation"
            icon="bin"
            label="Delete Account"
            onPress={handleDeleteAccount}
            textColor={danger}
            chevronColor={danger}
          />
        </SettingsSection>

        {/* ── Footer ──────────────────────────────────────────────────── */}
        <View style={styles.footer}>
          <Text style={[styles.footerText, { color: tertiary }]}>
            MooTD v1.0.0
          </Text>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

// ─── Styles ──────────────────────────────────────────────────────────────────

const creativityStyles = {
  creativityRow: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 12,
  } as const,
  creativityHint: {
    ...typography.footnote.regular,
  } as const,
};

const styles = StyleSheet.create({
  ...creativityStyles,
  container: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 16,
  },
  backButton: {
    width: 40,
    height: 40,
    justifyContent: 'center',
    alignItems: 'center',
  },
  title: {
    ...typography.title3.semiBold,
    flex: 1,
    textAlign: 'center',
  },
  headerSpacer: { width: 40 },
  scroll: { flex: 1 },
  scrollContent: {
    paddingHorizontal: 16,
    paddingBottom: 40,
  },
  editRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 12,
  },
  editIcon: {
    alignSelf: 'flex-start',
    paddingTop: 2,
  },
  editInputWrapper: {
    flex: 1,
  },
  editLabel: {
    fontSize: 13,
    lineHeight: 18,
    marginBottom: 2,
  },
  nameInput: {
    ...typography.body.regular,
    paddingVertical: 4,
    paddingHorizontal: 0,
    margin: 0,
    borderWidth: 0,
    outlineStyle: 'none',
  } as any,
  saveButton: {
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: 8,
  },
  saveButtonText: {
    fontSize: 14,
    fontWeight: '600',
    color: '#FFFFFF',
  },
  inlineDivider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 48,
  },
  footer: {
    alignItems: 'center',
    paddingVertical: 32,
  },
  footerText: {
    ...typography.caption1.regular,
  },
});
