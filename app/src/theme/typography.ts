import { TextStyle } from 'react-native';

// Font families from Figma
export const fontFamilies = {
  montserrat: {
    regular: 'MontserratAlternates-Regular',
    semiBold: 'MontserratAlternates-SemiBold',
  },
} as const;

// Typography styles from Figma
export const typography = {
  largeTitle: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 34,
      fontWeight: '400',
      lineHeight: 41,
      letterSpacing: 0.4,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 34,
      fontWeight: '600',
      lineHeight: 41,
      letterSpacing: 0.4,
    } as TextStyle,
  },
  title1: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 28,
      fontWeight: '400',
      lineHeight: 34,
      letterSpacing: 0.38,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 28,
      fontWeight: '600',
      lineHeight: 34,
      letterSpacing: 0.38,
    } as TextStyle,
  },
  title2: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 22,
      fontWeight: '400',
      lineHeight: 28,
      letterSpacing: -0.26,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 22,
      fontWeight: '600',
      lineHeight: 28,
      letterSpacing: -0.26,
    } as TextStyle,
  },
  title3: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 19,
      fontWeight: '400',
      lineHeight: 25,
      letterSpacing: -0.45,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 19,
      fontWeight: '600',
      lineHeight: 25,
      letterSpacing: -0.45,
    } as TextStyle,
  },
  headline: {
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 17,
      fontWeight: '600',
      lineHeight: 22,
      letterSpacing: -0.43,
    } as TextStyle,
  },

  body: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 17,
      fontWeight: '400',
      lineHeight: 26,
      letterSpacing: -0.43,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 17,
      fontWeight: '600',
      lineHeight: 26,
      letterSpacing: -0.43,
    } as TextStyle,
  },
  callout: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 16,
      fontWeight: '400',
      lineHeight: 23,
      letterSpacing: -0.31,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 16,
      fontWeight: '600',
      lineHeight: 23,
      letterSpacing: -0.31,
    } as TextStyle,
  },
  subheadline: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 15,
      fontWeight: '400',
      lineHeight: 20,
      letterSpacing: -0.23,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 15,
      fontWeight: '600',
      lineHeight: 20,
      letterSpacing: -0.23,
    } as TextStyle,
  },
  footnote: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 13,
      fontWeight: '400',
      lineHeight: 18,
      letterSpacing: -0.08,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 13,
      fontWeight: '600',
      lineHeight: 18,
      letterSpacing: -0.08,
    } as TextStyle,
  },
  caption1: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 12,
      fontWeight: '400',
      lineHeight: 16,
      letterSpacing: 0,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 12,
      fontWeight: '600',
      lineHeight: 16,
      letterSpacing: 0,
    } as TextStyle,
  },
  caption2: {
    regular: {
      fontFamily: fontFamilies.montserrat.regular,
      fontSize: 11,
      fontWeight: '400',
      lineHeight: 13,
      letterSpacing: 0.06,
    } as TextStyle,
    semiBold: {
      fontFamily: fontFamilies.montserrat.semiBold,
      fontSize: 11,
      fontWeight: '600',
      lineHeight: 13,
      letterSpacing: 0.06,
    } as TextStyle,
  },
} as const;

export type Typography = typeof typography;
