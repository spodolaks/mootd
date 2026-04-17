import React from 'react';
import { View, StyleSheet, ViewStyle } from 'react-native';
import { SelectableItem } from './SelectableItem';
import type { SelectableItemVariant } from './types';

export interface SelectableItemOption {
  /**
   * Unique identifier for the option
   */
  id: string;
  /**
   * The label text to display
   */
  label: string;
  /**
   * The variant of the selectable item
   * @default 'simple'
   */
  variant?: SelectableItemVariant;
  /**
   * Whether the option is disabled
   */
  disabled?: boolean;
}

export interface SelectableItemGroupProps {
  /**
   * Array of options to display
   */
  options: SelectableItemOption[];
  /**
   * The currently selected value(s)
   * For single select: string
   * For multi select: string[]
   */
  value: string | string[];
  /**
   * Callback when selection changes
   * For single select: (value: string) => void
   * For multi select: (values: string[]) => void
   */
  onChange: (value: string | string[]) => void;
  /**
   * Whether multiple items can be selected
   * @default false
   */
  multiple?: boolean;
  /**
   * Gap between items
   * @default 12
   */
  gap?: number;
  /**
   * Custom style for the container
   */
  style?: ViewStyle;
}

export const SelectableItemGroup: React.FC<SelectableItemGroupProps> = ({
  options,
  value,
  onChange,
  multiple = false,
  gap = 12,
  style,
}) => {
  const selectedValues = Array.isArray(value) ? value : [value];

  const handlePress = (optionId: string) => {
    if (multiple) {
      const currentValues = Array.isArray(value) ? value : [];
      const isSelected = currentValues.includes(optionId);

      if (isSelected) {
        onChange(currentValues.filter(v => v !== optionId));
      } else {
        onChange([...currentValues, optionId]);
      }
    } else {
      onChange(optionId);
    }
  };

  return (
    <View style={[styles.container, { gap }, style]}>
      {options.map(option => (
        <SelectableItem
          key={option.id}
          label={option.label}
          variant={option.variant}
          selected={selectedValues.includes(option.id)}
          disabled={option.disabled}
          onPress={() => handlePress(option.id)}
        />
      ))}
    </View>
  );
};

const styles = StyleSheet.create({
  container: {
    flexDirection: 'column',
  },
});
