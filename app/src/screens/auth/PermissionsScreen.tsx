import { GradientButton, Text, Toggle } from '@/src/components';
import { NotificationBell } from '@/src/components/icons/NotificationBell';
import { useColorScheme } from '@/src/hooks';
import { backgrounds, labels, separators } from '@/src/theme/colors';
import React, { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

interface PermissionsScreenProps {
  /**
   * Callback when user completes the permissions screen
   */
  onGetStarted?: () => void;
}

export const PermissionsScreen: React.FC<PermissionsScreenProps> = ({
  onGetStarted,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const [locationEnabled, setLocationEnabled] = useState(false);
  const [notificationEnabled, setNotificationEnabled] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];
  const separatorColor = separators.primary[colorScheme];

  const handleGetStarted = () => {
    onGetStarted?.();
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        {/* Main Content */}
        <View style={styles.mainContent}>
          {/* Icon */}
          <View style={styles.iconContainer}>
            <NotificationBell width={162} height={206} />
          </View>

          {/* Title */}
          <Text variant="title1" weight="semiBold" style={styles.title}>
            Get notified
          </Text>

          {/* Description */}
          <View style={styles.descriptionContainer}>
            <Text
              variant="body"
              style={[styles.description, { color: secondaryTextColor }]}
            >
              For best experience....
            </Text>
          </View>

          {/* Toggles */}
          <View style={styles.togglesContainer}>
            {/* Location Toggle */}
            <View style={[styles.toggleRow, { borderBottomColor: separatorColor }]}>
              <Text variant="body">Location</Text>
              <Toggle
                value={locationEnabled}
                onValueChange={setLocationEnabled}
              />
            </View>

            {/* Notification Toggle */}
            <View style={styles.toggleRow}>
              <Text variant="body">Notification</Text>
              <Toggle
                value={notificationEnabled}
                onValueChange={setNotificationEnabled}
              />
            </View>
          </View>
        </View>

        {/* Bottom Button */}
        <View style={styles.buttonContainer}>
          <GradientButton
            label="Get Started"
            onPress={handleGetStarted}
          />
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
