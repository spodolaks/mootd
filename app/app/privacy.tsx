/**
 * Privacy & Data screen (P2-06 / mootd-admin#23).
 *
 * Two GDPR-tier actions:
 *   - Delete account: DELETE /v1/privacy/self. Wipes every per-
 *     user record (wardrobe, outfits, moodboards, feedback,
 *     events, traces, llm_calls, detection_runs, budget) plus
 *     the user document. After this call the user can no
 *     longer log in.
 *   - Export data: GET /v1/privacy/export. Returns a JSON ZIP
 *     with every record we hold about the user. Mobile UX is a
 *     "request export → backend prepares it" flow — we hit the
 *     endpoint, surface the byte count, and tell the user they
 *     can also fetch it from a browser if they need the file
 *     itself (RN's ZIP download UX is awkward enough to defer
 *     to a follow-up).
 */
import React, { useState } from 'react';
import {
  Alert,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { Icon } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { apiClient } from '@/src/data/api/client';
import { useAuthStore } from '@/src/store';
import {
  accents,
  backgrounds,
  grays,
  labels,
  separators,
} from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';

interface PurgeReport {
  userId: string;
  purgedAt: string;
  collections: Record<string, number>;
  total: number;
}

export default function PrivacyScreen() {
  const colorScheme = useColorScheme() ?? 'light';
  const router = useRouter();
  const signOut = useAuthStore((s) => s.signOut);

  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [exportBusy, setExportBusy] = useState(false);
  const expectedConfirm = 'DELETE';

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const cardBg = grays.gray5[colorScheme];
  const dividerColor = separators.primary[colorScheme];
  const dangerColor = accents.red[colorScheme];

  const handleDelete = async () => {
    if (confirm !== expectedConfirm) return;
    Alert.alert(
      'Delete account?',
      'This will permanently erase your wardrobe, outfits, moodboards, and account history. You will be signed out immediately. This cannot be undone.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: async () => {
            setBusy(true);
            try {
              const report = await apiClient.delete<PurgeReport>(
                '/v1/privacy/self',
              );
              Alert.alert(
                'Account deleted',
                `${report.total} records removed across ${Object.keys(report.collections).length} categories. Signing you out.`,
                [
                  {
                    text: 'OK',
                    onPress: () => {
                      signOut();
                      router.replace('/');
                    },
                  },
                ],
              );
            } catch (e) {
              Alert.alert(
                'Delete failed',
                e instanceof Error ? e.message : 'Unknown error. Please try again later.',
              );
            } finally {
              setBusy(false);
            }
          },
        },
      ],
    );
  };

  const handleExport = async () => {
    setExportBusy(true);
    try {
      // Hit the endpoint to confirm it works + give the user a
      // "yes, your data is exportable" signal. The byte count
      // comes from Content-Length on the response.
      // Mobile ZIP-download UX is deferred — for now we point
      // the user at the web export route.
      const res = await fetch(
        `${process.env.EXPO_PUBLIC_API_URL}/v1/privacy/export`,
        {
          method: 'GET',
          headers: {
            Authorization: `Bearer ${useAuthStore.getState().session?.accessToken ?? ''}`,
          },
        },
      );
      if (!res.ok) {
        throw new Error(`Export failed: HTTP ${res.status}`);
      }
      const buf = await res.arrayBuffer();
      Alert.alert(
        'Export ready',
        `We prepared a ${(buf.byteLength / 1024).toFixed(1)} KB archive. Mobile download is coming in a future update — for now, please request the export from a desktop browser.`,
      );
    } catch (e) {
      Alert.alert(
        'Export failed',
        e instanceof Error ? e.message : 'Please try again later.',
      );
    } finally {
      setExportBusy(false);
    }
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()} style={styles.backButton}>
          <Icon name="chevron-left" size={20} color={textColor} />
        </Pressable>
        <Text style={[styles.title, { color: textColor }]}>Privacy & Data</Text>
      </View>

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}
      >
        <View style={[styles.section, { backgroundColor: cardBg }]}>
          <Text style={[styles.sectionTitle, { color: textColor }]}>
            Export your data
          </Text>
          <Text style={[styles.sectionBody, { color: secondaryText }]}>
            Download every record we hold about you — wardrobe items, outfits,
            moodboards, and account history — as a JSON archive.
          </Text>
          <Pressable
            style={[styles.actionButton, { borderTopColor: dividerColor }]}
            onPress={handleExport}
            disabled={exportBusy}
          >
            <Icon name="image" size={18} color={textColor} />
            <Text style={[styles.actionButtonText, { color: textColor }]}>
              {exportBusy ? 'Preparing export…' : 'Request data export'}
            </Text>
          </Pressable>
        </View>

        <View style={[styles.section, { backgroundColor: cardBg }]}>
          <Text style={[styles.sectionTitle, { color: dangerColor }]}>
            Delete account
          </Text>
          <Text style={[styles.sectionBody, { color: secondaryText }]}>
            Permanently erase every record about you. This cannot be undone.
            Type{' '}
            <Text style={{ ...typography.body.semiBold, color: textColor }}>
              {expectedConfirm}
            </Text>{' '}
            below to confirm.
          </Text>
          <TextInput
            value={confirm}
            onChangeText={setConfirm}
            autoCapitalize="characters"
            autoCorrect={false}
            placeholder={expectedConfirm}
            placeholderTextColor={secondaryText}
            style={[
              styles.input,
              { color: textColor, borderColor: dividerColor },
            ]}
            editable={!busy}
          />
          <Pressable
            style={[
              styles.actionButton,
              styles.dangerButton,
              { borderTopColor: dividerColor },
              (busy || confirm !== expectedConfirm) && styles.disabled,
            ]}
            onPress={handleDelete}
            disabled={busy || confirm !== expectedConfirm}
          >
            <Icon name="bin" size={18} color={dangerColor} />
            <Text style={[styles.actionButtonText, { color: dangerColor }]}>
              {busy ? 'Deleting…' : 'Delete my account'}
            </Text>
          </Pressable>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 16,
    gap: 8,
  },
  backButton: {
    padding: 8,
  },
  title: {
    ...typography.title2.semiBold,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingHorizontal: 16,
    paddingBottom: 40,
    gap: 16,
  },
  section: {
    borderRadius: 16,
    padding: 16,
    gap: 12,
  },
  sectionTitle: {
    ...typography.headline.semiBold,
  },
  sectionBody: {
    ...typography.subheadline.regular,
  },
  input: {
    ...typography.body.regular,
    borderWidth: 1,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  actionButton: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: 12,
    borderTopWidth: StyleSheet.hairlineWidth,
    gap: 12,
  },
  dangerButton: {},
  actionButtonText: {
    ...typography.body.semiBold,
  },
  disabled: {
    opacity: 0.5,
  },
});
