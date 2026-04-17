import { LinearGradient } from 'expo-linear-gradient';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import {
  Animated,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import type { TextInputProps, ViewStyle } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { Icon } from '../../icons/Icon';
import { backgrounds, fills, gradients, grays, labels } from '../../../theme/colors';
import { typography } from '../../../theme/typography';
import { radius } from '../../../theme/radius';
import { spacing } from '../../../theme/spacing';
import { brandsRepository } from '@/src/data/repositories';

const HEIGHT = 54;
const BORDER_WIDTH = 1.5;
// Inner radius slightly smaller to account for the border
const INNER_RADIUS = radius.full - BORDER_WIDTH;

const GRADIENT_COLORS = gradients.primary.colors;
const GRADIENT_LOCATIONS = gradients.primary.locations;


export interface BrandSearchInputProps extends Omit<TextInputProps, 'style'> {
  /** Called when the clear (×) button is pressed */
  onClear?: () => void;
  /** Outer wrapper style (e.g. margins) */
  style?: ViewStyle;
}

export const BrandSearchInput: React.FC<BrandSearchInputProps> = ({
  value,
  onChangeText,
  onClear,
  onBlur,
  placeholder = 'Search brand…',
  style,
  ...rest
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const [isFocused, setIsFocused] = useState(false);
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const inputRef = useRef<TextInput>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const blurTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (blurTimerRef.current) clearTimeout(blurTimerRef.current);
    };
  }, []);

  // Animate gradient opacity so it fades in on focus
  const gradientOpacity = useRef(new Animated.Value(0)).current;

  const handleFocus = () => {
    setIsFocused(true);
    Animated.timing(gradientOpacity, {
      toValue: 1,
      duration: 180,
      useNativeDriver: true,
    }).start();
  };

  const handleBlur = () => {
    setIsFocused(false);
    // Delay hiding so a tap on a suggestion registers first
    blurTimerRef.current = setTimeout(() => setSuggestions([]), 150);
    Animated.timing(gradientOpacity, {
      toValue: 0,
      duration: 180,
      useNativeDriver: true,
    }).start();
  };

  const handleChangeText = useCallback((text: string) => {
    onChangeText?.(text);
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (text.trim().length === 0) {
      setSuggestions([]);
      return;
    }
    debounceRef.current = setTimeout(() => {
      void brandsRepository.searchBrands(text.trim()).then((results) => {
        setSuggestions(results);
      }).catch(() => {
        setSuggestions([]);
      });
    }, 300);
  }, [onChangeText]);

  const handleClear = () => {
    onChangeText?.('');
    setSuggestions([]);
    onClear?.();
    inputRef.current?.focus();
  };

  const handleSuggestionPress = (brand: string) => {
    onChangeText?.(brand);
    setSuggestions([]);
  };

  const iconColor = isFocused ? labels.primary[colorScheme] : grays.gray[colorScheme];
  const inputBg = fills.tertiary[colorScheme];
  const textColor = labels.primary[colorScheme];
  const placeholderColor = labels.tertiary[colorScheme];
  const dropdownBg = backgrounds.primary[colorScheme];

  const showSuggestions = suggestions.length > 0;

  return (
    <View style={[styles.container, style]}>
      <Pressable onPress={() => inputRef.current?.focus()} style={styles.wrapper}>
        {/* Gradient border layer — fades in on focus */}
        <Animated.View style={[StyleSheet.absoluteFill, { opacity: gradientOpacity, borderRadius: radius.full }]}>
          <LinearGradient
            colors={GRADIENT_COLORS}
            locations={GRADIENT_LOCATIONS}
            start={{ x: 0, y: 0.5 }}
            end={{ x: 1, y: 0.5 }}
            style={[StyleSheet.absoluteFill, { borderRadius: radius.full }]}
          />
        </Animated.View>

        {/* Static unfocused border */}
        <Animated.View style={[StyleSheet.absoluteFill, { opacity: Animated.subtract(1, gradientOpacity), borderRadius: radius.full }]}>
          <View style={[StyleSheet.absoluteFill, { borderRadius: radius.full, borderWidth: BORDER_WIDTH, borderColor: fills.secondary[colorScheme] }]} />
        </Animated.View>

        {/* Input background + content */}
        <View style={[styles.inner, { backgroundColor: inputBg, borderRadius: INNER_RADIUS }]}>
          <Icon name="search" size={20} color={iconColor} />
          <TextInput
            ref={inputRef}
            style={[typography.body.regular, styles.input, { color: textColor }]}
            value={value}
            onChangeText={handleChangeText}
            onFocus={handleFocus}
            onBlur={(e) => { handleBlur(); onBlur?.(e); }}
            placeholder={placeholder}
            placeholderTextColor={placeholderColor}
            returnKeyType="search"
            autoCorrect={false}
            autoCapitalize="words"
            {...rest}
          />
          {!!value && (
            <Pressable onPress={handleClear} hitSlop={8} style={styles.clearButton}>
              <Icon name="close" size={16} color={grays.gray[colorScheme]} />
            </Pressable>
          )}
        </View>
      </Pressable>

      {showSuggestions && (
        <View style={[styles.dropdown, { backgroundColor: dropdownBg }]}>
          <ScrollView keyboardShouldPersistTaps="always" bounces={false}>
            {suggestions.map((brand) => (
              <Pressable
                key={brand}
                style={({ pressed }) => [
                  styles.suggestionItem,
                  pressed && { backgroundColor: fills.tertiary[colorScheme] },
                ]}
                onPress={() => handleSuggestionPress(brand)}
              >
                <Text style={[typography.body.regular, { color: textColor }]}>
                  {brand}
                </Text>
              </Pressable>
            ))}
          </ScrollView>
        </View>
      )}
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    zIndex: 10,
  },
  wrapper: {
    height: HEIGHT,
    borderRadius: radius.full,
    padding: BORDER_WIDTH,
  },
  inner: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.md,
    gap: spacing.sm,
  },
  input: {
    flex: 1,
    height: '100%',
    padding: 0,
  },
  clearButton: {
    justifyContent: 'center',
    alignItems: 'center',
  },
  dropdown: {
    position: 'absolute',
    top: HEIGHT + 4,
    left: 0,
    right: 0,
    maxHeight: 200,
    borderRadius: radius.lg,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 2 },
    shadowOpacity: 0.12,
    shadowRadius: 8,
    elevation: 4,
    overflow: 'hidden',
  },
  suggestionItem: {
    paddingHorizontal: spacing.md,
    paddingVertical: 14,
  },
});
