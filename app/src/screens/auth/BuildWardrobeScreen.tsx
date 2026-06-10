import React from 'react';
import { View, StyleSheet, ActivityIndicator, Pressable } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useColorScheme } from '@/src/hooks';
import { Text, GradientButton, Button } from '@/src/components';
import { ProfilePlaceholder } from '@/src/components/icons/ProfilePlaceholder';
import { accents, backgrounds, fills, labels } from '@/src/theme/colors';
import { radius } from '@/src/theme/radius';
import { spacing } from '@/src/theme/spacing';

interface BuildWardrobeScreenProps {
  onTakePhoto?: () => void;
  onUploadFromGallery?: () => void;
  onBrowseCatalog?: () => void;
  /** True while a background detection job this screen started is in flight. */
  isLoading?: boolean;
  /**
   * mootd#163 — staged status text for the in-flight detection
   * ("Uploading photo...", "Detecting clothing items...", …). Shown in the
   * inline, non-blocking progress card instead of an opaque full-screen
   * overlay. Null when nothing is running.
   */
  detectionStatus?: string | null;
  /**
   * mootd#163 — abort the in-flight detection and return to the photo-pick
   * step. Wired to the Cancel button on the progress card.
   */
  onCancelDetection?: () => void;
  /** Error message to display when detection fails */
  error?: string | null;
}

export const BuildWardrobeScreen: React.FC<BuildWardrobeScreenProps> = ({
  onTakePhoto,
  onUploadFromGallery,
  onBrowseCatalog,
  isLoading = false,
  detectionStatus = null,
  onCancelDetection,
  error = null,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];
  const errorColor = accents.red[colorScheme];
  const cardBg = fills.tertiary[colorScheme];
  const statusTextColor = labels.secondary[colorScheme];
  const spinnerColor = labels.primary[colorScheme];
  const cancelColor = accents.blue[colorScheme];

  const handleTakePhoto = () => {
    onTakePhoto?.();
  };

  const handleUploadFromGallery = () => {
    onUploadFromGallery?.();
  };

  const handleBrowseCatalog = () => {
    onBrowseCatalog?.();
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        {/* Upper section - Illustration and Text */}
        <View style={styles.upperSection}>
          {/* Profile Placeholder Illustration */}
          <View style={styles.illustrationContainer}>
            <ProfilePlaceholder width={162} height={206} />
          </View>

          {/* Title and Description */}
          <View style={styles.textContainer}>
            <Text variant="title2" weight="semiBold" style={styles.title}>
              Let&apos;s build your{'\n'}wardrobe your way
            </Text>
            <Text variant="subheadline" style={[styles.description, { color: secondaryTextColor }]}>
              Take a mirror selfie and we&apos;ll detect your outfit automatically, or upload from
              gallery
            </Text>
          </View>
        </View>

        {/* mootd#163 — inline, non-blocking detection progress. Replaces the
            old full-screen, uncancelable LoadingOverlay: the user sees staged
            status text and can Cancel at any time to return to this step. */}
        {isLoading && (
          <View
            style={[styles.statusCard, { backgroundColor: cardBg }]}
            accessibilityRole="progressbar"
            accessibilityLabel={detectionStatus ?? 'Detecting clothing'}>
            <View style={styles.statusRow}>
              <ActivityIndicator size="small" color={spinnerColor} />
              <Text variant="subheadline" weight="semiBold" style={[styles.statusText, { color: statusTextColor }]}>
                {detectionStatus ?? 'Detecting clothing…'}
              </Text>
            </View>
            <Pressable
              onPress={() => onCancelDetection?.()}
              hitSlop={8}
              testID="cancel-detection"
              accessibilityRole="button"
              accessibilityLabel="Cancel detection and go back">
              <Text variant="footnote" weight="semiBold" style={{ color: cancelColor }}>
                Cancel
              </Text>
            </Pressable>
          </View>
        )}

        {/* Error message */}
        {error && !isLoading && (
          <View style={styles.errorContainer}>
            <Text variant="footnote" style={[styles.errorText, { color: errorColor }]}>
              {error}
            </Text>
          </View>
        )}

        {/* Bottom section - Action Buttons */}
        <View style={styles.buttonsContainer}>
          <GradientButton
            label="Take a full body photo"
            icon="camera"
            onPress={handleTakePhoto}
            style={styles.gradientButton}
            disabled={isLoading}
          />

          <Button
            label="Upload from gallery"
            icon="upload"
            variant="secondary"
            size="lg"
            onPress={handleUploadFromGallery}
            style={styles.secondaryButton}
            disabled={isLoading}
          />

          <Button
            label="Browse items catalog"
            icon="search"
            variant="secondary"
            size="lg"
            onPress={handleBrowseCatalog}
            style={styles.secondaryButton}
            disabled={isLoading}
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
    justifyContent: 'space-between',
  },
  upperSection: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  illustrationContainer: {
    marginBottom: 32,
  },
  textContainer: {
    alignItems: 'center',
  },
  title: {
    textAlign: 'center',
    marginBottom: 12,
  },
  description: {
    textAlign: 'center',
    paddingHorizontal: 24,
  },
  buttonsContainer: {
    width: '100%',
    gap: 10,
    paddingBottom: 16,
  },
  gradientButton: {
    marginBottom: -20,
  },
  secondaryButton: {
    width: '100%',
  },
  errorContainer: {
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  errorText: {
    textAlign: 'center',
  },
  // mootd#163 — inline detection progress card.
  statusCard: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginHorizontal: 16,
    marginBottom: spacing.sm,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.md,
    borderRadius: radius.lg,
    gap: spacing.md,
  },
  statusRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    flexShrink: 1,
  },
  statusText: {
    flexShrink: 1,
  },
});
