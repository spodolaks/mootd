import { Button, Input, Text } from '@/src/components';
import { Icon } from '@/src/components/icons/Icon';
import { SegmentedProgressBar } from '@/src/components/ui/SegmentedProgressBar';
import { useColorScheme } from '@/src/hooks';
import { useWardrobeStore } from '@/src/store';
import { backgrounds, fills, labels } from '@/src/theme/colors';
import { radius } from '@/src/theme/radius';
import React, { useEffect, useMemo, useState } from 'react';
import { Image, Keyboard, Platform, ScrollView, StyleSheet, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

interface TraitSelectionScreenProps {
  onBack?: () => void;
  onNextItem?: () => void;
  onComplete?: () => void;
}

export const TraitSelectionScreen: React.FC<TraitSelectionScreenProps> = ({
  onBack,
  onNextItem,
  onComplete,
}) => {
  const colorScheme = useColorScheme() ?? 'light';

  const { items, currentStepIndex, getTotalSteps, setTraitValue, nextStep, isLastStep } =
    useWardrobeStore();

  const currentItem = items[currentStepIndex] ?? null;

  const [keyboardHeight, setKeyboardHeight] = useState(0);
  const [imgError, setImgError] = useState(false);

  useEffect(() => {
    setImgError(false);
  }, [currentStepIndex]);

  useEffect(() => {
    const showEvent = Platform.OS === 'ios' ? 'keyboardWillShow' : 'keyboardDidShow';
    const hideEvent = Platform.OS === 'ios' ? 'keyboardWillHide' : 'keyboardDidHide';

    const showSub = Keyboard.addListener(showEvent, e => {
      setKeyboardHeight(e.endCoordinates.height);
    });
    const hideSub = Keyboard.addListener(hideEvent, () => {
      setKeyboardHeight(0);
    });

    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  const totalSteps = getTotalSteps();
  const isLast = isLastStep();

  const backgroundColor = backgrounds.primary[colorScheme];
  const secondaryTextColor = labels.tertiary[colorScheme];
  const placeholderBg = fills.tertiary[colorScheme];
  const placeholderColor = labels.tertiary[colorScheme];

  // mootd#54 — Continue stays disabled until the user has SOMETHING
  // to verify the model with. We previously required every trait
  // filled, but the orchestrator's GarmentDescription schema doesn't
  // produce style/occasion (judgment fields a single-photo vision
  // model can't tag), so requiring them dead-ended every import on
  // an empty form. Relaxed gate: as long as at least one trait
  // carries a non-empty value (either auto-filled by detection or
  // typed by the user), the user has confirmed they saw the model's
  // output and Continue enables.
  const hasAnyTraitFilled = useMemo(() => {
    if (!currentItem) return false;
    return currentItem.traits.some(
      trait => trait.selectedValue !== null && trait.selectedValue.trim() !== ''
    );
  }, [currentItem]);

  const handleBack = () => {
    onBack?.();
  };

  const handleNext = () => {
    if (!currentItem) return;
    if (isLast) {
      onComplete?.();
    } else {
      nextStep();
      onNextItem?.();
    }
  };

  const handleTraitChange = (traitId: string, value: string) => {
    setTraitValue(traitId, value);
  };

  if (!currentItem) {
    return (
      <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
        <View style={styles.emptyContainer}>
          <View style={styles.emptyContent}>
            <Text variant="body">No item to configure</Text>
          </View>
          <View style={styles.emptyButtonContainer}>
            <Button
              label="Back"
              variant="secondary"
              size="lg"
              onPress={handleBack}
              style={styles.emptyBackButton}
            />
          </View>
        </View>
      </SafeAreaView>
    );
  }

  const hasImage = !!currentItem.imageSource && !imgError;

  return (
    <SafeAreaView style={[styles.container, { backgroundColor }]} edges={['top', 'bottom']}>
      <View style={styles.content}>
        <SegmentedProgressBar
          totalSegments={totalSteps}
          currentSegment={currentStepIndex}
          style={styles.progressBar}
          withFade
        />

        <ScrollView
          style={styles.scrollView}
          contentContainerStyle={[
            styles.scrollContent,
            { paddingBottom: keyboardHeight > 0 ? keyboardHeight - 60 : 16 },
          ]}
          showsVerticalScrollIndicator={false}
          keyboardShouldPersistTaps="handled">
          <Text variant="body" style={[styles.itemCounter, { color: secondaryTextColor }]}>
            Item {currentStepIndex + 1} of {totalSteps}
          </Text>

          <View style={styles.titleSection}>
            <Text variant="title1" weight="semiBold" style={styles.title}>
              {currentItem.label}
            </Text>
          </View>

          {/* Item Image */}
          <View style={[styles.imageContainer, { backgroundColor: placeholderBg }]}>
            {hasImage ? (
              <Image
                source={currentItem.imageSource}
                style={styles.image}
                resizeMode="cover"
                onError={() => setImgError(true)}
              />
            ) : (
              <Icon name="closet" size={48} color={placeholderColor} />
            )}
          </View>

          <View style={styles.traitsHeader}>
            <Text variant="body" weight="semiBold">
              Traits
            </Text>
            <Text
              variant="subheadline"
              style={[styles.traitsDescription, { color: secondaryTextColor }]}>
              Review the detected traits and edit anything that looks off.
            </Text>
          </View>

          {currentItem.traits.map(trait => (
            <Input
              key={trait.id}
              title={trait.name}
              placeholder={`Enter ${trait.name.toLowerCase()}`}
              value={trait.selectedValue ?? ''}
              onChangeText={value => handleTraitChange(trait.id, value)}
            />
          ))}
        </ScrollView>

        <View style={styles.buttonsContainer}>
          <Button
            label="Back"
            variant="secondary"
            size="lg"
            onPress={handleBack}
            style={styles.backButton}
          />
          <Button
            label={isLast ? 'Done' : 'Next'}
            variant="primary"
            size="lg"
            onPress={handleNext}
            disabled={!hasAnyTraitFilled}
            style={styles.nextButton}
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
  emptyContainer: {
    flex: 1,
    paddingHorizontal: 16,
  },
  emptyContent: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  emptyButtonContainer: {
    paddingBottom: 24,
  },
  emptyBackButton: {
    width: '100%',
  },
  progressBar: {
    marginTop: 8,
  },
  itemCounter: {
    textAlign: 'center',
    marginTop: 24,
    marginBottom: 16,
  },
  titleSection: {
    marginBottom: 16,
  },
  title: {
    textAlign: 'center',
  },
  imageContainer: {
    width: '60%',
    aspectRatio: 3 / 4,
    alignSelf: 'center',
    borderRadius: radius.xl,
    marginTop: 16,
    marginBottom: 48,
    overflow: 'hidden',
    justifyContent: 'center',
    alignItems: 'center',
  },
  image: {
    width: '100%',
    height: '100%',
  },
  traitsHeader: {
    marginBottom: 16,
  },
  traitsDescription: {
    marginTop: 4,
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {},
  buttonsContainer: {
    flexDirection: 'row',
    gap: 12,
    paddingBottom: 24,
    paddingTop: 8,
  },
  backButton: {
    flex: 1,
  },
  nextButton: {
    flex: 1,
  },
});
