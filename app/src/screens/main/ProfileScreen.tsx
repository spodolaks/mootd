import React from 'react';
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { Icon, Button } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { useAuthStore, usePreferencesStore, useDetectionJobStore } from '@/src/store';
import {
  backgrounds,
  button,
  fills,
  grays,
  labels,
  separators,
} from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';

export const ProfileScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const session = useAuthStore((s) => s.session);
  const signOut = useAuthStore((s) => s.signOut);
  const hasActiveJob = useDetectionJobStore((s) => s.hasActiveJob);
  const jobCount = useDetectionJobStore((s) => s.jobs.length);
  const customDisplayName = usePreferencesStore((s) => s.displayName);
  const persistedEmail = usePreferencesStore((s) => s.email);

  // Prefer the user-edited display name, fall back to Google profile name
  const displayName = customDisplayName || user?.name || 'Unknown User';
  const email = user?.email || persistedEmail || '';

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const tertiaryText = labels.tertiary[colorScheme];
  const cardBg = grays.gray5[colorScheme];
  const dividerColor = separators.primary[colorScheme];
  const dangerColor = '#FF3B30';

  const handleSignOut = () => {
    signOut();
    router.replace('/');
  };

  // First letter(s) of display name for fallback avatar
  const initials = displayName !== 'Unknown User'
    ? displayName
        .split(' ')
        .map((n) => n[0])
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
        contentContainerStyle={styles.scrollContent}
        showsVerticalScrollIndicator={false}
      >
        {/* Avatar + Name Section */}
        <View style={styles.profileSection}>
          {user?.avatarUrl ? (
            <Image source={{ uri: user.avatarUrl }} style={styles.avatar} cachePolicy="memory-disk" />
          ) : (
            <View style={[styles.avatarFallback, { backgroundColor: cardBg }]}>
              <Text style={[styles.avatarInitials, { color: textColor }]}>
                {initials}
              </Text>
            </View>
          )}
          <Text style={[styles.userName, { color: textColor }]}>
            {displayName}
          </Text>
          <Text style={[styles.userEmail, { color: secondaryText }]}>
            {user?.email ?? ''}
          </Text>
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
            badgeColor={hasActiveJob() ? '#007AFF' : '#34C759'}
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
            icon="info"
            label="About MooTD"
            textColor={textColor}
            dividerColor={dividerColor}
          />
        </View>

        {/* Sign Out */}
        <View style={styles.signOutSection}>
          <Pressable
            style={[styles.signOutButton, { backgroundColor: cardBg }]}
            onPress={handleSignOut}
        
          >
            <Text style={[styles.signOutText, { color: dangerColor }]}>
              Sign Out
            </Text>
          </Pressable>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
};

interface MenuItemProps {
  icon: 'settings' | 'info' | 'user' | 'sync' | 'camera' | 'star';
  label: string;
  textColor: string;
  dividerColor: string;
  showDivider?: boolean;
  badge?: string | null;
  badgeColor?: string;
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
  onPress,
}) => (
  <>
    <Pressable
      style={styles.menuItem}
      onPress={onPress}
    >
      <Icon name={icon} size={20} color={textColor} />
      <Text style={[styles.menuItemText, { color: textColor }]}>{label}</Text>
      {badge && (
        <View style={[styles.badge, { backgroundColor: badgeColor }]}>
          <Text style={styles.badgeText}>{badge}</Text>
        </View>
      )}
      <Icon name="chevron-right" size={16} color={textColor} />
    </Pressable>
    {showDivider && (
      <View style={[styles.divider, { backgroundColor: dividerColor }]} />
    )}
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
