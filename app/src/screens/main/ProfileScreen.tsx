import React, { useEffect, useState } from 'react';
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
import { SegmentedControl } from '@/src/components/ui/SegmentedControl/SegmentedControl';
import { useColorScheme } from '@/src/hooks';
import { useAuthStore, usePreferencesStore, useDetectionJobStore } from '@/src/store';
import { apiClient } from '@/src/data/api/client';
import {
  accents,
  backgrounds,
  button,
  fills,
  grays,
  labels,
  separators,
} from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';
import { useTabContentBottomPadding } from '@/app/(main)/_layout';

export const ProfileScreen: React.FC = () => {
  const colorScheme = useColorScheme() ?? 'light';
  const tabBottomPadding = useTabContentBottomPadding();
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const session = useAuthStore((s) => s.session);
  const signOut = useAuthStore((s) => s.signOut);
  const hasActiveJob = useDetectionJobStore((s) => s.hasActiveJob);
  const jobCount = useDetectionJobStore((s) => s.jobs.length);
  const customDisplayName = usePreferencesStore((s) => s.displayName);
  const persistedEmail = usePreferencesStore((s) => s.email);

  // Profile gender — not carried in the auth session, so fetch it
  // from /v1/user/profile on mount. null until loaded (card hidden).
  const [gender, setGender] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    apiClient
      .get<{ gender?: string }>('/v1/user/profile')
      .then((p) => {
        if (!cancelled) setGender(p.gender ?? null);
      })
      .catch(() => {
        /* leave unset — the gender card just won't render */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleGenderChange = (value: string) => {
    const previous = gender;
    setGender(value); // optimistic
    apiClient.put('/v1/user/profile', { gender: value }).catch(() => {
      setGender(previous); // revert on failure
    });
  };

  // Prefer the user-edited display name, fall back to Google profile name
  const displayName = customDisplayName || user?.name || 'Unknown User';
  const email = user?.email || persistedEmail || '';

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
        contentContainerStyle={[styles.scrollContent, { paddingBottom: tabBottomPadding }]}
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

        {/* Gender — drives which archetype-default fillers moodboards use. */}
        {gender && (
          <View style={[styles.menuSection, { backgroundColor: cardBg }]}>
            <View style={styles.genderRow}>
              <Text style={[styles.genderLabel, { color: secondaryText }]}>
                Styling for
              </Text>
              <SegmentedControl
                options={[
                  { label: 'Female', value: 'female' },
                  { label: 'Male', value: 'male' },
                ]}
                selectedValue={gender}
                onValueChange={handleGenderChange}
              />
            </View>
          </View>
        )}

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
  icon: 'settings' | 'info' | 'user' | 'sync' | 'camera' | 'star' | 'privacy';
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
  genderRow: {
    paddingHorizontal: 16,
    paddingVertical: 14,
    gap: 10,
  },
  genderLabel: {
    ...typography.subheadline.regular,
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
