import React from 'react';
import { Pressable, View, Image } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { grays, labels } from '../../../theme/colors';
import { Text } from '../Text';
import { Icon } from '../../icons';
import { getStyles } from './styles';
import type { ClothingItemCardProps } from './types';

const DARK_GREY = '#3A3A3C';

export const ClothingItemCard: React.FC<ClothingItemCardProps> = ({
  label,
  selected = false,
  imageSource,
  darkBackground = false,
  onPress,
  disabled = false,
  style,
}) => {
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const placeholderColor = grays.gray5[colorScheme];
  const textColor = labels.primary[colorScheme];
  const checkBadgeBackground = grays.black[colorScheme];
  const checkColor = grays.white[colorScheme];
  const borderColor = selected ? grays.black[colorScheme] : 'transparent';
  const cardBg = darkBackground ? DARK_GREY : placeholderColor;

  return (
    <View style={[styles.container, style]}>
      <Pressable
        style={styles.touchable}
        onPress={onPress}
        disabled={disabled}
      >
        <View
          style={[
            styles.imageContainer,
            {
              backgroundColor: cardBg,
              borderWidth: selected ? 2 : 0,
              borderColor,
            },
          ]}
        >
          {imageSource ? (
            <Image source={imageSource} style={styles.image} resizeMode={darkBackground ? 'contain' : 'cover'} />
          ) : (
            <View style={[styles.placeholder, { backgroundColor: placeholderColor }]} />
          )}

          {selected && (
            <View style={[styles.checkBadge, { backgroundColor: checkBadgeBackground }]}>
              <Icon name="check" size={16} color={checkColor} />
            </View>
          )}
        </View>

        <Text
          variant="subheadline"
          style={[styles.label, { color: textColor }]}
          numberOfLines={1}
        >
          {label}
        </Text>
      </Pressable>
    </View>
  );
};
