import React from 'react';
import { render, screen } from '@testing-library/react-native';
import { LoadingOverlay } from '../LoadingOverlay';

describe('LoadingOverlay', () => {
  it('renders with default message', () => {
    render(<LoadingOverlay />);
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it('renders with custom message', () => {
    render(<LoadingOverlay message="Detecting clothing..." />);
    expect(screen.getByText('Detecting clothing...')).toBeTruthy();
  });
});
