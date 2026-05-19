import { useCallback, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { SegmentedControl } from '@/src/components/ui/SegmentedControl/SegmentedControl';
import { useColorScheme } from '@/src/hooks';
import { apiClient } from '@/src/data/api/client';
import { wardrobeRepository } from '@/src/data/repositories';
import { backgrounds, button, labels } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';

/**
 * Onboarding gender step. Shown once after sign-in when the user has
 * no profile gender yet. The choice drives which archetype-default
 * fillers their moodboards mix in (a male user sees male + unisex
 * fillers, a female user female + unisex) and the default gender
 * stamped on items they add to their wardrobe.
 */
export default function OnboardingGender() {
  const router = useRouter();
  const colorScheme = useColorScheme() ?? 'light';
  const [gender, setGender] = useState<'male' | 'female'>('female');
  const [isSaving, setIsSaving] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const buttonBg = button.primary.background[colorScheme];
  const buttonFg = button.primary.foreground[colorScheme];

  const handleContinue = useCallback(async () => {
    setIsSaving(true);
    try {
      await apiClient.put('/v1/user/profile', { gender });
    } catch (e) {
      Alert.alert(
        'Could not save',
        e instanceof Error ? e.message : 'Please try again.',
      );
      setIsSaving(false);
      return;
    }
    // Gender saved — continue the usual post-auth routing.
    try {
      const { items } = await wardrobeRepository.getItems();
      router.replace(items.length === 0 ? '/build-wardrobe' : '/(main)/moodboard');
    } catch {
      router.replace('/build-wardrobe');
    }
  }, [gender, router]);

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]}>
      <View style={styles.content}>
        <Text style={[styles.title, { color: textColor }]}>
          Who are we styling?
        </Text>
        <Text style={[styles.subtitle, { color: secondaryText }]}>
          This tailors your outfit suggestions. You can change it later
          in your profile.
        </Text>
        <SegmentedControl
          options={[
            { label: 'Female', value: 'female' },
            { label: 'Male', value: 'male' },
          ]}
          selectedValue={gender}
          onValueChange={(value) => setGender(value as 'male' | 'female')}
          disabled={isSaving}
          style={styles.segmented}
        />
      </View>
      <Pressable
        style={[
          styles.button,
          { backgroundColor: buttonBg },
          isSaving && styles.buttonDisabled,
        ]}
        onPress={() => {
          void handleContinue();
        }}
        disabled={isSaving}
        testID="onboarding-gender-continue"
        accessibilityLabel="Save gender and continue"
      >
        {isSaving ? (
          <ActivityIndicator color={buttonFg} />
        ) : (
          <Text style={[styles.buttonText, { color: buttonFg }]}>Continue</Text>
        )}
      </Pressable>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    paddingHorizontal: 24,
    justifyContent: 'space-between',
  },
  content: {
    flex: 1,
    justifyContent: 'center',
    gap: 16,
  },
  title: {
    ...typography.title1.semiBold,
  },
  subtitle: {
    ...typography.body.regular,
    marginBottom: 8,
  },
  segmented: {
    marginTop: 8,
  },
  button: {
    height: 54,
    borderRadius: 16,
    justifyContent: 'center',
    alignItems: 'center',
    marginBottom: 24,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  buttonText: {
    ...typography.body.semiBold,
  },
});
