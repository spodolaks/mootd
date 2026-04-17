import React, { useState } from 'react';
import { View, TextInput, Text, Pressable } from 'react-native';
import { useColorScheme } from '@/src/hooks';
import { typography } from '../../../theme';
import { labels } from '../../../theme/colors';
import { getStyles } from './styles';
import type { InputProps } from './types';

export const Input: React.FC<InputProps> = ({
  title,
  description,
  error,
  disabled = false,
  leftIcon,
  rightIcon,
  onLeftIconPress,
  onRightIconPress,
  style,
  multiline,
  ...props
}) => {
  const [isFocused, setIsFocused] = useState(false);
  const colorScheme = useColorScheme() ?? 'light';
  const styles = getStyles(colorScheme);

  const renderIcon = (
    icon: React.ReactNode,
    onPress?: () => void,
    position: 'left' | 'right' = 'left'
  ) => {
    if (!icon) return null;

    const iconStyle = position === 'left' ? styles.leftIcon : styles.rightIcon;

    if (onPress && !disabled) {
      return (
        <Pressable onPress={onPress} style={iconStyle} hitSlop={8} accessibilityRole="button">
          {icon}
        </Pressable>
      );
    }

    return <View style={iconStyle}>{icon}</View>;
  };

  return (
    <View style={styles.container}>
      {(title || description) && (
        <View style={styles.headerContainer}>
          {title && (
            <Text
              style={[typography.callout.semiBold, styles.title, disabled && styles.titleDisabled]}>
              {title}
            </Text>
          )}
          {description && (
            <Text
              style={[
                typography.caption1.regular,
                styles.description,
                disabled && styles.descriptionDisabled,
              ]}>
              {description}
            </Text>
          )}
        </View>
      )}
      <View
        style={[
          styles.inputContainer,
          isFocused && styles.inputContainerFocused,
          error && styles.inputContainerError,
          disabled && styles.inputContainerDisabled,
          multiline && styles.inputContainerMultiline,
        ]}>
        {renderIcon(leftIcon, onLeftIconPress, 'left')}
        <TextInput
          style={[
            typography.body.regular,
            styles.input,
            !!leftIcon && styles.inputWithLeftIcon,
            !!rightIcon && styles.inputWithRightIcon,
            multiline && styles.inputMultiline,
            disabled && styles.inputDisabled,
            style,
          ]}
          onFocus={() => setIsFocused(true)}
          onBlur={() => setIsFocused(false)}
          placeholderTextColor={labels.tertiary[colorScheme]}
          editable={!disabled}
          multiline={multiline}
          textAlignVertical={multiline ? 'top' : 'center'}
          {...props}
        />
        {renderIcon(rightIcon, onRightIconPress, 'right')}
      </View>
      {error && <Text style={[typography.caption1.regular, styles.errorText]}>{error}</Text>}
    </View>
  );
};
