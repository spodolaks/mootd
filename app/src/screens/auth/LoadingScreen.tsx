import { LoadingSpinner, Text } from '@/src/components';
import { useColorScheme } from '@/src/hooks';
import { backgrounds } from '@/src/theme/colors';
import React, { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

interface LoadingScreenProps {
  /**
   * Text to display below the spinner
   */
  text?: string;
  /**
   * Callback when loading is complete
   */
  onComplete?: () => void;
  /**
   * Duration of loading in milliseconds
   */
  duration?: number;
}

export const LoadingScreen: React.FC<LoadingScreenProps> = ({
  text = 'Generating',
  onComplete,
  duration = 3000,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const backgroundColor = backgrounds.primary[colorScheme];

  useEffect(() => {
    const timer = setTimeout(() => {
      onComplete?.();
    }, duration);

    return () => {
      clearTimeout(timer);
    };
  }, [onComplete, duration]);

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        <LoadingSpinner style={styles.spinner} />
        <Text variant="title1" weight="semiBold" style={styles.text}>
          {text}
        </Text>
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
    justifyContent: 'center',
    alignItems: 'center',
  },
  spinner: {
    marginBottom: 24,
  },
  text: {
    textAlign: 'center',
  },
});
