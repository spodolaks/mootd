import { useCallback, useState } from 'react';
import { ActivityIndicator, Alert, Pressable, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { useColorScheme } from '@/src/hooks';
import { apiClient } from '@/src/data/api/client';
import { wardrobeRepository } from '@/src/data/repositories';
import { backgrounds, button, labels, separators } from '@/src/theme/colors';
import { typography } from '@/src/theme/typography';

type Gender = 'female' | 'male' | 'unisex';

const GENDER_OPTIONS: readonly { label: string; value: Gender }[] = [
  { label: 'Female', value: 'female' },
  { label: 'Male', value: 'male' },
  { label: "As long as it's stylish", value: 'unisex' },
];

/**
 * Onboarding gender step. Shown once after sign-in when the user has
 * no profile gender yet. The choice drives which archetype-default
 * fillers their moodboards mix in:
 *   - male   → male + unisex fillers
 *   - female → female + unisex fillers
 *   - unisex ("as long as it's stylish") → no restriction, every filler
 * It also seeds the default gender stamped on items they add. Defaults
 * to "unisex" — the neutral, no-assumption choice.
 */
export default function OnboardingGender() {
  const router = useRouter();
  const colorScheme = useColorScheme() ?? 'light';
  const [gender, setGender] = useState<Gender>('unisex');
  const [isSaving, setIsSaving] = useState(false);

  const backgroundColor = backgrounds.primary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const secondaryText = labels.secondary[colorScheme];
  const borderColor = separators.primary[colorScheme];
  const buttonBg = button.primary.background[colorScheme];
  const buttonFg = button.primary.foreground[colorScheme];

  const handleContinue = useCallback(async () => {
    setIsSaving(true);
    try {
      await apiClient.put('/v1/user/profile', { gender });
    } catch (e) {
      Alert.alert('Could not save', e instanceof Error ? e.message : 'Please try again.');
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
        <Text style={[styles.title, { color: textColor }]}>Who are we styling?</Text>
        <Text style={[styles.subtitle, { color: secondaryText }]}>
          This tailors your outfit suggestions. You can change it anytime in Preferences.
        </Text>
        <View style={styles.options}>
          {GENDER_OPTIONS.map(opt => {
            const selected = gender === opt.value;
            return (
              <Pressable
                key={opt.value}
                onPress={() => setGender(opt.value)}
                disabled={isSaving}
                style={[
                  styles.option,
                  selected ? { backgroundColor: buttonBg } : { borderWidth: 1.5, borderColor },
                ]}
                testID={`onboarding-gender-${opt.value}`}
                accessibilityLabel={opt.label}
                accessibilityRole="radio"
                accessibilityState={{ selected }}>
                <Text style={[styles.optionLabel, { color: selected ? buttonFg : textColor }]}>
                  {opt.label}
                </Text>
              </Pressable>
            );
          })}
        </View>
      </View>
      <Pressable
        style={[styles.button, { backgroundColor: buttonBg }, isSaving && styles.buttonDisabled]}
        onPress={() => {
          void handleContinue();
        }}
        disabled={isSaving}
        testID="onboarding-gender-continue"
        accessibilityLabel="Save gender and continue">
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
  options: {
    gap: 10,
    marginTop: 8,
  },
  option: {
    height: 56,
    borderRadius: 14,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 16,
  },
  optionLabel: {
    ...typography.body.semiBold,
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
