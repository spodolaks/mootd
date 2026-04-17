import React from 'react';
import { View, StyleSheet } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useColorScheme } from '@/src/hooks';
import { Text, GradientButton, Button, LoadingOverlay } from '@/src/components';
import { ProfilePlaceholder } from '@/src/components/icons/ProfilePlaceholder';
import { backgrounds, labels } from '@/src/theme/colors';

interface BuildWardrobeScreenProps {
  onTakePhoto?: () => void;
  onUploadFromGallery?: () => void;
  onBrowseCatalog?: () => void;
  /** Show a loading overlay while detection is in progress */
  isLoading?: boolean;
  /** Error message to display when detection fails */
  error?: string | null;
}

export const BuildWardrobeScreen: React.FC<BuildWardrobeScreenProps> = ({
  onTakePhoto,
  onUploadFromGallery,
  onBrowseCatalog,
  isLoading = false,
  error = null,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];

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
              Let's build your{'\n'}wardrobe your way
            </Text>
            <Text
              variant="subheadline"
              style={[styles.description, { color: secondaryTextColor }]}
            >
              Take a mirror selfie and we'll detect your outfit automatically, or upload from gallery
            </Text>
          </View>
        </View>

        {/* Error message */}
        {error && (
          <View style={styles.errorContainer}>
            <Text variant="footnote" style={styles.errorText}>
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

      {/* Loading overlay */}
      {isLoading && <LoadingOverlay message="Detecting clothing..." />}
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
    color: '#FF3B30',
    textAlign: 'center',
  },
});
