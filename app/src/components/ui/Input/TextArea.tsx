import React from 'react';
import { Input } from './Input';
import type { InputProps } from './types';

export interface TextAreaProps extends Omit<InputProps, 'multiline'> {
  /** Number of lines (affects min height) */
  numberOfLines?: number;
}

export const TextArea: React.FC<TextAreaProps> = ({
  numberOfLines = 4,
  placeholder = 'Write here...',
  ...props
}) => {
  return <Input multiline numberOfLines={numberOfLines} placeholder={placeholder} {...props} />;
};
