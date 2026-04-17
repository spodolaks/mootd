import React from 'react';
import { View, StyleSheet, Text as RNText } from 'react-native';
import { Image } from 'expo-image';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useColorScheme } from '@/src/hooks';
import { Text, GradientButton } from '@/src/components';
import { accents, backgrounds, labels } from '@/src/theme/colors';
import { fontFamilies } from '@/src/theme/typography';

interface WelcomeScreenProps {
  onGoogleSignIn?: () => void;
  isLoading?: boolean;
  errorMessage?: string | null;
}

export const WelcomeScreen: React.FC<WelcomeScreenProps> = ({
  onGoogleSignIn,
  isLoading = false,
  errorMessage = null,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const errorColor = accents.red[colorScheme];

  const handleGoogleSignIn = () => {
    onGoogleSignIn?.();
  };

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <Image
        source={require('@/assets/images/bokeh-background.png')}
        style={[
          StyleSheet.absoluteFillObject,
          styles.backgroundImage,
          { opacity: colorScheme === 'dark' ? 0.88 : 0.52 },
        ]}
        contentFit="cover"
        cachePolicy="memory-disk"
        accessible={false}
      />
      <View
        pointerEvents="none"
        style={[
          StyleSheet.absoluteFillObject,
          styles.backgroundWash,
          {
            backgroundColor:
              colorScheme === 'dark' ? 'rgba(0, 0, 0, 0.18)' : 'rgba(255, 255, 255, 0.12)',
          },
        ]}
      />
      <View style={styles.content}>
        {/* Centered block with logo, tagline, and button */}
        <View style={styles.centerBlock}>
          {/* Logo and Tagline */}
          <View style={styles.logoContainer}>
            <RNText style={[styles.logo, { color: textColor }]}>MOOTD</RNText>
            <Text variant="subheadline" style={styles.tagline}>
              Your Personal Fashion Assistant
            </Text>
          </View>

          {/* Google Sign In Button */}
          <View style={styles.buttonContainer}>
            <GradientButton
              label={isLoading ? 'Signing in...' : 'Continue with Google'}
              icon="google"
              onPress={handleGoogleSignIn}
              disabled={isLoading}
            />
            {errorMessage ? (
              <Text
                variant="footnote"
                style={[styles.errorText, { color: errorColor }]}
              >
                {errorMessage}
              </Text>
            ) : null}
          </View>
        </View>
      </View>
    </SafeAreaView>
  );
};

const styles = StyleSheet.create({
  container: {
    flex: 1,
    overflow: 'hidden',
  },
  backgroundImage: {
    zIndex: 0,
  },
  backgroundWash: {
    zIndex: 0,
  },
  content: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 16,
    zIndex: 1,
  },
  centerBlock: {
    alignItems: 'center',
    width: '100%',
  },
  logoContainer: {
    alignItems: 'stretch',
  },
  logo: {
    fontFamily: fontFamilies.montserrat.regular,
    fontSize: 48,
    letterSpacing: 4,
    textAlign: 'center',
  },
  tagline: {
    textAlign: 'center',
    marginTop: 2,
  },
  buttonContainer: {
    width: '100%',
    marginTop: 40,
  },
  errorText: {
    textAlign: 'center',
    marginTop: 12,
  },
});
