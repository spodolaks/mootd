import { GradientButton, Text, Toggle } from '@/src/components';
import { NotificationBell } from '@/src/components/icons/NotificationBell';
import { useColorScheme } from '@/src/hooks';
import { usePreferencesStore } from '@/src/store/preferencesStore';
import { backgrounds, labels, separators } from '@/src/theme/colors';
import * as Linking from 'expo-linking';
import * as Location from 'expo-location';
import * as Notifications from 'expo-notifications';
import React, { useCallback, useEffect, useState } from 'react';
import { Alert, AppState, Platform, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

interface PermissionsScreenProps {
  /**
   * Callback when user completes the permissions screen
   */
  onGetStarted?: () => void;
}

/**
 * Onboarding permissions screen (mootd#46).
 *
 * Toggles wire to real OS permission requests via expo-location +
 * expo-notifications. Previously the toggles only flipped local
 * state — visually filled but the user never got prompted, so the
 * UI lied to them.
 *
 * Behaviour per toggle:
 *   - Mount: read current OS state, reflect as the toggle's
 *     position. Re-reads on app foreground so a Settings.app round
 *     trip syncs back without a hard reload.
 *   - Toggle ON: request the OS permission. If granted → toggle
 *     stays ON. If denied → show an Alert offering to open
 *     Settings (the only way back into the prompt once the user
 *     has dismissed it on iOS).
 *   - Toggle OFF: the OS doesn't expose an "ungrant from app" API,
 *     so this only flips the in-app preference (the user can still
 *     receive pushes per the OS, the app just won't trigger them).
 */
export const PermissionsScreen: React.FC<PermissionsScreenProps> = ({ onGetStarted }) => {
  const colorScheme = useColorScheme() ?? 'light';

  const [locationEnabled, setLocationEnabled] = useState(false);
  const [notificationEnabled, setNotificationEnabled] = useState(false);
  const setPushNotifications = usePreferencesStore(s => s.setPushNotifications);

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];
  const separatorColor = separators.primary[colorScheme];

  // Read live OS permission state. Called on mount + on foreground
  // so a Settings.app round-trip syncs the toggles without a manual
  // reload.
  const syncFromOS = useCallback(async () => {
    try {
      const loc = await Location.getForegroundPermissionsAsync();
      setLocationEnabled(loc.status === 'granted');
    } catch {
      // expo-location can throw on web (where the API is unavailable);
      // leave the toggle off in that case.
      setLocationEnabled(false);
    }
    try {
      const notif = await Notifications.getPermissionsAsync();
      setNotificationEnabled(notif.status === 'granted');
    } catch {
      setNotificationEnabled(false);
    }
  }, []);

  useEffect(() => {
    void syncFromOS();
    const sub = AppState.addEventListener('change', state => {
      if (state === 'active') {
        void syncFromOS();
      }
    });
    return () => sub.remove();
  }, [syncFromOS]);

  /**
   * promptOpenSettings is the universal "you denied us, here's how
   * to recover" affordance. iOS won't show the system permission
   * sheet a second time once the user has tapped Don't Allow —
   * Settings is the only path back. Linking.openSettings() drops
   * the user directly into our app's permission page.
   */
  const promptOpenSettings = useCallback((label: string) => {
    Alert.alert(
      `${label} permission denied`,
      Platform.OS === 'ios'
        ? `Open Settings to turn on ${label.toLowerCase()} for Mootd.`
        : `Open Settings to grant ${label.toLowerCase()} permission to Mootd.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Open Settings',
          onPress: () => {
            void Linking.openSettings();
          },
        },
      ]
    );
  }, []);

  const handleLocationToggle = useCallback(
    async (next: boolean) => {
      if (!next) {
        // OS doesn't let us revoke; keep visual state in sync.
        // Toggling off just stops us asking for the position from
        // the in-app code paths (none today; future weather logic
        // will read this preference).
        setLocationEnabled(false);
        return;
      }
      try {
        const result = await Location.requestForegroundPermissionsAsync();
        if (result.status === 'granted') {
          setLocationEnabled(true);
          return;
        }
        // 'denied' is the user tapping Don't Allow; 'undetermined' is
        // a prompt that didn't fire (web). Either way → settings.
        setLocationEnabled(false);
        if (!result.canAskAgain) {
          promptOpenSettings('Location');
        }
      } catch (err) {
        setLocationEnabled(false);
        Alert.alert(
          'Location permission unavailable',
          err instanceof Error ? err.message : 'Try again later.'
        );
      }
    },
    [promptOpenSettings]
  );

  const handleNotificationToggle = useCallback(
    async (next: boolean) => {
      if (!next) {
        setNotificationEnabled(false);
        // Reflect the user-level intent in the preference store so
        // future server-driven push send paths can opt-out without
        // hitting the OS.
        setPushNotifications(false);
        return;
      }
      try {
        const result = await Notifications.requestPermissionsAsync();
        if (result.status === 'granted') {
          setNotificationEnabled(true);
          setPushNotifications(true);
          return;
        }
        setNotificationEnabled(false);
        if (!result.canAskAgain) {
          promptOpenSettings('Notification');
        }
      } catch (err) {
        setNotificationEnabled(false);
        Alert.alert(
          'Notification permission unavailable',
          err instanceof Error ? err.message : 'Try again later.'
        );
      }
    },
    [promptOpenSettings, setPushNotifications]
  );

  const handleGetStarted = () => {
    onGetStarted?.();
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        <View style={styles.mainContent}>
          <View style={styles.iconContainer}>
            <NotificationBell width={162} height={206} />
          </View>

          <Text variant="title1" weight="semiBold" style={styles.title}>
            Get notified
          </Text>

          <View style={styles.descriptionContainer}>
            <Text variant="body" style={[styles.description, { color: secondaryTextColor }]}>
              Turn on notifications so you never miss your daily outfit, plus location for
              weather-aware recommendations.
            </Text>
          </View>

          <View style={styles.togglesContainer}>
            <View style={[styles.toggleRow, { borderBottomColor: separatorColor }]}>
              <Text variant="body">Location</Text>
              <Toggle
                value={locationEnabled}
                onValueChange={v => {
                  void handleLocationToggle(v);
                }}
              />
            </View>

            <View style={styles.toggleRow}>
              <Text variant="body">Notification</Text>
              <Toggle
                value={notificationEnabled}
                onValueChange={v => {
                  void handleNotificationToggle(v);
                }}
              />
            </View>
          </View>
        </View>

        <View style={styles.buttonContainer}>
          <GradientButton label="Get Started" onPress={handleGetStarted} />
        </View>
      </View>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  content: {
    flex: 1,
    paddingHorizontal: 16,
  },
  mainContent: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  iconContainer: {
    marginBottom: 24,
  },
  title: {
    textAlign: 'center',
    marginBottom: 12,
  },
  descriptionContainer: {
    marginBottom: 24,
  },
  description: {
    textAlign: 'center',
  },
  togglesContainer: {
    width: '100%',
    marginTop: 8,
  },
  toggleRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'transparent',
  },
  buttonContainer: {
    paddingBottom: 16,
  },
});
