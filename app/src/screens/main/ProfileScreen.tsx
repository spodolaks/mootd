import React from 'react';
import { Alert, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { Image } from 'expo-image';
import Constants from 'expo-constants';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { Icon } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { useAuthStore, usePreferencesStore, useDetectionJobStore } from '@/src/store';
import { accents, backgrounds, grays, labels, separators } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';

// App version sourced from app.json (expo config), matching the read in the
// root layout. Shown on the "About MooTD" row in place of a chevron, since
// there's no separate About destination to navigate to.
const APP_VERSION = (Constants?.expoConfig?.version as string | undefined) ?? '0.0.0';

export const ProfileScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const tabBottomPadding = useTabContentBottomPadding();
  const router = useRouter();
  const user = useAuthStore(s => s.user);
  const session = useAuthStore(s => s.session);
  const signOut = useAuthStore(s => s.signOut);
  const hasActiveJob = useDetectionJobStore(s => s.hasActiveJob);
  const jobCount = useDetectionJobStore(s => s.jobs.length);
  const customDisplayName = usePreferencesStore(s => s.displayName);

  // Prefer the user-edited display name, fall back to Google profile name
  const displayName = customDisplayName || user?.name || 'Unknown User';

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const tertiaryText = labels.tertiary[colorScheme];
  const cardBg = grays.gray5[colorScheme];
  const dividerColor = separators.primary[colorScheme];
  const dangerColor = accents.red[colorScheme];
  const activeBadgeColor = accents.blue[colorScheme];
  const idleBadgeColor = accents.green[colorScheme];

  const handleSignOut = () => {
    // #135: await signOut() (which clears auth state + every per-user store)
    // before navigating, so index.tsx doesn't bounce us back into (main) while
    // still authenticated.
    const doSignOut = async () => {
      await signOut();
      router.replace('/');
    };

    if (Platform.OS === 'web') {
      // Alert.alert is a no-op on web – use window.confirm instead
      if (confirm('Sign out of your account?')) {
        void doSignOut();
      }
    } else {
      Alert.alert('Sign Out', 'Are you sure you want to sign out?', [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Sign Out',
          style: 'destructive',
          onPress: () => {
            void doSignOut();
          },
        },
      ]);
    }
  };

  // First letter(s) of display name for fallback avatar
  const initials =
    displayName !== 'Unknown User'
      ? displayName
          .split(' ')
          .map(n => n[0])
          .join('')
          .toUpperCase()
          .slice(0, 2)
      : '?';

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top']}>
      {/* Header */}
      <View style={styles.header}>
        <Text style={[styles.title, { color: textColor }]}>Profile</Text>
      </View>

      <ScrollView
        style={styles.scrollView}
        contentContainerStyle={[styles.scrollContent, { paddingBottom: tabBottomPadding }]}
        showsVerticalScrollIndicator={false}>
        {/* Avatar + Name Section */}
        <View style={styles.profileSection}>
          {user?.avatarUrl ? (
            <Image
              source={{ uri: user.avatarUrl }}
              style={styles.avatar}
              cachePolicy="memory-disk"
            />
          ) : (
            <View style={[styles.avatarFallback, { backgroundColor: cardBg }]}>
              <Text style={[styles.avatarInitials, { color: textColor }]}>{initials}</Text>
            </View>
          )}
          <Text style={[styles.userName, { color: textColor }]}>{displayName}</Text>
          <Text style={[styles.userEmail, { color: secondaryText }]}>{user?.email ?? ''}</Text>
          {session?.mode && (
            <View style={[styles.modeBadge, { backgroundColor: cardBg }]}>
              <Text style={[styles.modeBadgeText, { color: tertiaryText }]}>
                {session.mode === 'mock' ? 'Mock Mode' : 'Connected'}
              </Text>
            </View>
          )}
        </View>

        {/* Menu Sections */}
        {/* Detection Activity — always visible, badge when active */}
        <View style={[styles.menuSection, { backgroundColor: cardBg }]}>
          <MenuItem
            icon="camera"
            label="Detection Activity"
            textColor={textColor}
            dividerColor={dividerColor}
            badge={jobCount > 0 ? String(jobCount) : null}
            badgeColor={hasActiveJob() ? activeBadgeColor : idleBadgeColor}
            onPress={() => {
              router.push('/detection-activity');
            }}
          />
        </View>

        <View style={[styles.menuSection, { backgroundColor: cardBg }]}>
          <MenuItem
            icon="star"
            label="Style Analysis"
            textColor={textColor}
            dividerColor={dividerColor}
            showDivider
            onPress={() => {
              router.push('/style-analysis');
            }}
          />
          <MenuItem
            icon="settings"
            label="Preferences"
            textColor={textColor}
            dividerColor={dividerColor}
            showDivider
            onPress={() => {
              router.push('/preferences');
            }}
          />
          <MenuItem
            icon="privacy"
            label="Privacy & Data"
            textColor={textColor}
            dividerColor={dividerColor}
            showDivider
            onPress={() => {
              router.push('/privacy');
            }}
          />
          <MenuItem
            icon="info"
            label="About MooTD"
            textColor={textColor}
            dividerColor={dividerColor}
            value={`v${APP_VERSION}`}
            valueColor={tertiaryText}
            showChevron={false}
          />
        </View>

        {/* Sign Out */}
        <View style={styles.signOutSection}>
          <Pressable
            style={[styles.signOutButton, { backgroundColor: cardBg }]}
            onPress={handleSignOut}>
            <Text style={[styles.signOutText, { color: dangerColor }]}>Sign Out</Text>
          </Pressable>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

interface MenuItemProps {
  icon: 'settings' | 'info' | 'user' | 'sync' | 'camera' | 'star' | 'privacy';
  label: string;
  textColor: string;
  dividerColor: string;
  showDivider?: boolean;
  badge?: string | null;
  badgeColor?: string;
  /** Trailing read-only text (e.g. an app version) shown in place of a chevron. */
  value?: string;
  /** Trailing text color for `value`; defaults to `textColor`. */
  valueColor?: string;
  /**
   * Whether to render the trailing chevron. Defaults to true. A row with no
   * `onPress` destination should pass `false` so it doesn't look tappable.
   */
  showChevron?: boolean;
  onPress?: () => void;
}

const MenuItem: React.FC<MenuItemProps> = ({
  icon,
  label,
  textColor,
  dividerColor,
  showDivider = false,
  badge,
  badgeColor = '#007AFF',
  value,
  valueColor,
  showChevron = true,
  onPress,
}) => (
  <>
    <Pressable style={styles.menuItem} onPress={onPress} disabled={!onPress}>
      <Icon name={icon} size={20} color={textColor} />
      <Text style={[styles.menuItemText, { color: textColor }]}>{label}</Text>
      {badge && (
        <View style={[styles.badge, { backgroundColor: badgeColor }]}>
          <Text style={styles.badgeText}>{badge}</Text>
        </View>
      )}
      {value ? (
        <Text style={[styles.menuItemValue, { color: valueColor ?? textColor }]}>{value}</Text>
      ) : null}
      {showChevron && <Icon name="chevron-right" size={16} color={textColor} />}
    </Pressable>
    {showDivider && <View style={[styles.divider, { backgroundColor: dividerColor }]} />}
  </>
);

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 16,
  },
  title: {
    ...typography.largeTitle.semiBold,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    paddingHorizontal: 16,
    paddingBottom: 40,
  },
  profileSection: {
    alignItems: 'center',
    paddingVertical: 24,
    gap: 8,
  },
  avatar: {
    width: 88,
    height: 88,
    borderRadius: 44,
    marginBottom: 8,
  },
  avatarFallback: {
    width: 88,
    height: 88,
    borderRadius: 44,
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 8,
  },
  avatarInitials: {
    ...typography.title1.semiBold,
  },
  userName: {
    ...typography.title3.semiBold,
  },
  userEmail: {
    ...typography.subheadline.regular,
  },
  modeBadge: {
    marginTop: 4,
    paddingHorizontal: 12,
    paddingVertical: 4,
    borderRadius: 12,
  },
  modeBadgeText: {
    ...typography.caption1.regular,
  },
  menuSection: {
    borderRadius: 16,
    overflow: 'hidden',
    marginBottom: 24,
  },
  menuItem: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: 16,
    paddingVertical: 14,
    gap: 12,
  },
  menuItemText: {
    ...typography.body.regular,
    flex: 1,
  },
  menuItemValue: {
    ...typography.subheadline.regular,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 48,
  },
  signOutSection: {
    marginBottom: 24,
  },
  signOutButton: {
    height: 54,
    borderRadius: 16,
    justifyContent: 'center',
    alignItems: 'center',
  },
  signOutText: {
    ...typography.body.semiBold,
  },
  badge: {
    minWidth: 20,
    height: 20,
    borderRadius: 10,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 6,
  },
  badgeText: {
    color: '#FFFFFF',
    fontSize: 12,
    fontWeight: '600',
  },
});
