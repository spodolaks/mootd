import React from 'react';
import { Input } from './Input';
import type { InputProps } from './types';
import { Icon } from '../../icons/Icon';
import { grays } from '../../../theme/colors';
import { useColorScheme } from '@/src/hooks';

export interface SearchInputProps extends Omit<InputProps, 'leftIcon'> {
  /** Placeholder text (defaults to "Search") */
  placeholder?: string;
}

export const SearchInput: React.FC<SearchInputProps> = ({ placeholder = 'Search', ...props }) => {
  const colorScheme = useColorScheme() ?? 'light';
  const iconColor = grays.gray[colorScheme];

  return (
    <Input
      placeholder={placeholder}
      leftIcon={<Icon name="search" size={20} color={iconColor} />}
      {...props}
    />
  );
};
