import React, { useRef, useEffect, useState, useCallback } from 'react';
import { View, Text, Pressable, Animated, LayoutChangeEvent } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { getStyles } from './styles';
import type { SegmentedControlOption, SegmentedControlProps } from './types';

const CONTAINER_PADDING = 4;

export const SegmentedControl: React.FC<SegmentedControlProps> = ({
  options,
  selectedValue,
  onValueChange,
  disabled = false,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  // Normalize options to always be SegmentedControlOption[]
  const normalizedOptions: SegmentedControlOption[] = options.map(option =>
    typeof option === 'string' ? { label: option, value: option } : option
  );

  // Get the selected index
  const selectedIndex = normalizedOptions.findIndex(
    opt => (opt.value ?? opt.label) === selectedValue
  );

  // Track segment widths for the animated indicator
  const [segmentWidths, setSegmentWidths] = useState<number[]>([]);
  const [isLayoutReady, setIsLayoutReady] = useState(false);
  const animatedPosition = useRef(new Animated.Value(CONTAINER_PADDING)).current;

  // Calculate target position for a given index
  const calculatePosition = useCallback(
    (index: number): number => {
      let position = CONTAINER_PADDING;
      for (let i = 0; i < index; i++) {
        position += segmentWidths[i] || 0;
      }
      return position;
    },
    [segmentWidths]
  );

  // Update indicator position when selection or layout changes
  useEffect(() => {
    if (segmentWidths.length === normalizedOptions.length && selectedIndex >= 0) {
      const targetPosition = calculatePosition(selectedIndex);

      if (!isLayoutReady) {
        // Set initial position without animation
        animatedPosition.setValue(targetPosition);
        setIsLayoutReady(true);
      } else {
        // Animate to new position
        Animated.spring(animatedPosition, {
          toValue: targetPosition,
          useNativeDriver: false,
          tension: 68,
          friction: 10,
        }).start();
      }
    }
  }, [
    selectedIndex,
    segmentWidths,
    animatedPosition,
    normalizedOptions.length,
    calculatePosition,
    isLayoutReady,
  ]);

  const handleSegmentLayout = (index: number) => (event: LayoutChangeEvent) => {
    const { width } = event.nativeEvent.layout;
    setSegmentWidths(prev => {
      const newWidths = [...prev];
      newWidths[index] = width;
      return newWidths;
    });
  };

  const handlePress = (option: SegmentedControlOption) => {
    if (!disabled) {
      onValueChange(option.value ?? option.label);
    }
  };

  const indicatorWidth = segmentWidths[selectedIndex] || 0;

  return (
    <View style={[styles.container, disabled && styles.disabled, style]}>
      {/* Animated indicator */}
      {indicatorWidth > 0 && (
        <Animated.View
          style={[
            styles.indicator,
            {
              width: indicatorWidth,
              left: animatedPosition,
            },
          ]}
        />
      )}

      {/* Segments */}
      {normalizedOptions.map((option, index) => {
        const isSelected = (option.value ?? option.label) === selectedValue;
        return (
          <Pressable
            key={option.value ?? option.label}
            onPress={() => handlePress(option)}
            onLayout={handleSegmentLayout(index)}
            style={styles.segment}
            disabled={disabled}>
            <Text style={[styles.segmentText, isSelected && styles.segmentTextSelected]}>
              {option.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
};
