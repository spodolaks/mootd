import React from 'react';
import Svg, { Path, Line, Rect, Circle } from 'react-native-svg';
import { ViewStyle } from 'react-native';

export type IconName =
  | 'plus'
  | 'chevron-right'
  | 'chevron-left'
  | 'chevron-up'
  | 'chevron-down'
  | 'close'
  | 'calendar'
  | 'send'
  | 'mail'
  | 'image'
  | 'camera'
  | 'clock'
  | 'reset'
  | 'nav'
  | 'bin'
  | 'idea'
  | 'menu'
  | 'list'
  | 'star'
  | 'tag'
  | 'bell'
  | 'settings'
  | 'user'
  | 'privacy'
  | 'sun'
  | 'sunrise'
  | 'moon'
  | 'info'
  | 'home'
  | 'search'
  | 'cart'
  | 'sync'
  | 'umbrella'
  | 'cloud'
  | 'upload'
  | 'check'
  | 'lock'
  | 'lock-open'
  | 'compass'
  | 'file'
  | 'edit'
  | 'alert'
  | 'help'
  | 'closet'
  | 'google';

interface IconProps {
  name: IconName;
  size?: number;
  color?: string;
  style?: ViewStyle;
}

export function Icon({ name, size = 24, color = '#000000', style }: IconProps) {
  const strokeWidth = 2;
  const strokeLinecap = 'round';
  const strokeLinejoin = 'round';

  const renderIcon = () => {
    switch (name) {
      case 'plus':
        return (
          <>
            <Path
              d="M12 5V19"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M5 12H19"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'chevron-right':
        return (
          <Path
            d="M9 6L15 12L9 18"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'chevron-left':
        return (
          <Path
            d="M15 6L9 12L15 18"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'chevron-up':
        return (
          <Path
            d="M18 15L12 9L6 15"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'chevron-down':
        return (
          <Path
            d="M6 9L12 15L18 9"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'close':
        return (
          <>
            <Path
              d="M18 6L6 18"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M6 6L18 18"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'calendar':
        return (
          <>
            {/* Calendar body */}
            <Path
              d="M16 2C16.183 2 16.354 2.052 16.5 2.14L16.534 2.16C16.814 2.337 17 2.65 17 3V4H18C18.765 4 19.502 4.293 20.058 4.818C20.615 5.343 20.95 6.061 20.995 6.824L21 7V19C21 19.765 20.708 20.502 20.183 21.058C19.658 21.615 18.94 21.95 18.176 21.995L18 22H6C5.235 22 4.498 21.708 3.942 21.183C3.385 20.658 3.05 19.94 3.005 19.176L3 19V7C3 6.235 3.293 5.498 3.818 4.942C4.343 4.385 5.061 4.05 5.824 4.005L6 4H7V3C7 2.821 7.048 2.646 7.138 2.493C7.228 2.339 7.358 2.213 7.514 2.126L7.607 2.08L7.673 2.055L7.773 2.026L7.88 2.007L8 2C8.055 2 8.109 2.004 8.161 2.013L8.283 2.042L8.323 2.054L8.383 2.077C8.711 2.212 8.951 2.517 8.993 2.883L9 3V4H15V3C15 2.735 15.105 2.48 15.293 2.293C15.48 2.105 15.735 2 16 2ZM19 9H5V18.6C5 19.3 5.35 19.85 5.82 19.99L6 20H18C18.513 20 18.914 19.47 18.97 18.81L19 18.6V9Z"
              fill={color}
            />
            {/* Date dots - top row */}
            <Circle cx="8" cy="13" r="1" fill={color} />
            <Circle cx="12" cy="13" r="1" fill={color} />
            <Circle cx="16" cy="13" r="1" fill={color} />
            {/* Date dots - bottom row */}
            <Circle cx="8" cy="16" r="1" fill={color} />
            <Circle cx="12" cy="16" r="1" fill={color} />
          </>
        );

      case 'send':
        return (
          <>
            <Path
              d="M12 12L22 2"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M22 2L15.5 20C15.456 20.096 15.386 20.177 15.297 20.234C15.208 20.291 15.105 20.321 15 20.321C14.895 20.321 14.792 20.291 14.703 20.234C14.614 20.177 14.544 20.096 14.5 20L12 13L5 9.5C4.904 9.456 4.823 9.386 4.766 9.297C4.709 9.208 4.679 9.105 4.679 9C4.679 8.895 4.709 8.792 4.766 8.703C4.823 8.614 4.904 8.544 5 8.5L22 2Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'mail':
        return (
          <>
            <Path
              d="M3 7C3 6.47 3.211 5.961 3.586 5.586C3.961 5.211 4.47 5 5 5H19C19.53 5 20.039 5.211 20.414 5.586C20.789 5.961 21 6.47 21 7V17C21 17.53 20.789 18.039 20.414 18.414C20.039 18.789 19.53 19 19 19H5C4.47 19 3.961 18.789 3.586 18.414C3.211 18.039 3 17.53 3 17V7Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M3 7L12 13L21 7"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'image':
        return (
          <>
            <Circle cx="16" cy="8" r="1" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M4 6C4 5.204 4.316 4.441 4.879 3.879C5.441 3.316 6.204 3 7 3H17C17.796 3 18.559 3.316 19.121 3.879C19.684 4.441 20 5.204 20 6V18C20 18.796 19.684 19.559 19.121 20.121C18.559 20.684 17.796 21 17 21H7C6.204 21 5.441 20.684 4.879 20.121C4.316 19.559 4 18.796 4 18V6Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M4 16L9 11C9.928 10.107 11.072 10.107 12 11L17 16"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M15 14L16 13C16.928 12.107 18.072 12.107 19 13L20 14"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'camera':
        return (
          <>
            <Path
              d="M5 7H6C6.53 7 7.039 6.789 7.414 6.414C7.789 6.039 8 5.53 8 5C8 4.735 8.105 4.48 8.293 4.293C8.48 4.105 8.735 4 9 4H15C15.265 4 15.52 4.105 15.707 4.293C15.895 4.48 16 4.735 16 5C16 5.53 16.211 6.039 16.586 6.414C16.961 6.789 17.47 7 18 7H19C19.53 7 20.039 7.211 20.414 7.586C20.789 7.961 21 8.47 21 9V18C21 18.53 20.789 19.039 20.414 19.414C20.039 19.789 19.53 20 19 20H5C4.47 20 3.961 19.789 3.586 19.414C3.211 19.039 3 18.53 3 18V9C3 8.47 3.211 7.961 3.586 7.586C3.961 7.211 4.47 7 5 7Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Circle cx="12" cy="13" r="3" stroke={color} strokeWidth={strokeWidth} />
          </>
        );

      case 'clock':
        return (
          <>
            <Circle cx="12" cy="12" r="9" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M12 7V12L15 10"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'reset':
        return (
          <>
            <Path
              d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M3 3v5h5"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'nav':
        return (
          <Path
            d="M12 18.5L19.265 20.963C19.461 21.04 19.685 20.995 19.835 20.847C19.909 20.774 19.961 20.682 19.985 20.58C20.009 20.479 20.003 20.373 19.969 20.275L12 3L4.03 20.275C3.96 20.475 4.013 20.699 4.165 20.847C4.315 20.995 4.539 21.04 4.735 20.963L12 18.5Z"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'bin':
        return (
          <>
            <Path
              d="M4 7H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M10 11V17"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M14 11V17"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M5 7L6 19C6 19.53 6.211 20.039 6.586 20.414C6.961 20.789 7.47 21 8 21H16C16.53 21 17.039 20.789 17.414 20.414C17.789 20.039 18 19.53 18 19L19 7"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 7V4C9 3.735 9.105 3.48 9.293 3.293C9.48 3.105 9.735 3 10 3H14C14.265 3 14.52 3.105 14.707 3.293C14.895 3.48 15 3.735 15 4V7"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'idea':
        return (
          <>
            <Path
              d="M3 12H4M12 3V4M20 12H21M5.6 5.6L6.3 6.3M18.4 5.6L17.7 6.3"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 16C8.16 15.37 7.54 14.493 7.227 13.491C6.915 12.489 6.925 11.414 7.257 10.419C7.588 9.423 8.225 8.557 9.076 7.944C9.928 7.33 10.951 7 12 7C13.049 7 14.072 7.33 14.924 7.944C15.775 8.557 16.412 9.423 16.743 10.419C17.075 11.414 17.085 12.489 16.773 13.491C16.46 14.493 15.84 15.37 15 16C14.61 16.386 14.316 16.859 14.142 17.381C13.968 17.902 13.92 18.457 14 19C14 19.53 13.789 20.039 13.414 20.414C13.039 20.789 12.53 21 12 21C11.47 21 10.961 20.789 10.586 20.414C10.211 20.039 10 19.53 10 19C10.08 18.457 10.032 17.902 9.858 17.381C9.684 16.859 9.39 16.386 9 16Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9.7 17H14.3"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'menu':
        return (
          <>
            <Path
              d="M4 6H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M7 12H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M10 18H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'list':
        return (
          <>
            <Path
              d="M9 6H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 12H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 18H20"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Circle cx="5" cy="6" r="1" fill={color} />
            <Circle cx="5" cy="12" r="1" fill={color} />
            <Circle cx="5" cy="18" r="1" fill={color} />
          </>
        );

      case 'star':
        return (
          <Path
            d="M12 2L9.172 8.245L2.293 9.255L7.293 14.122L6.114 20.995L12.272 17.75L18.43 20.995L17.251 14.122L22.251 9.255L15.351 8.255L12.265 2.002L12 2Z"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'tag':
        return (
          <>
            <Circle cx="8.5" cy="8.5" r="1" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M4 6V11.172C4 11.702 4.211 12.211 4.586 12.586L12.296 20.296C12.748 20.748 13.361 21.002 14 21.002C14.639 21.002 15.252 20.748 15.704 20.296L21.296 14.704C21.748 14.252 22.002 13.639 22.002 13C22.002 12.361 21.748 11.748 21.296 11.296L13.586 3.586C13.211 3.211 12.702 3 12.172 3H7C6.204 3 5.441 3.316 4.879 3.879C4.316 4.441 4 5.204 4 6Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'bell':
        return (
          <>
            <Path
              d="M10 5C10 4.47 10.211 3.961 10.586 3.586C10.961 3.211 11.47 3 12 3C12.53 3 13.039 3.211 13.414 3.586C13.789 3.961 14 4.47 14 5C15.148 5.543 16.127 6.388 16.832 7.445C17.537 8.502 17.94 9.731 18 11V14C18.075 14.622 18.295 15.217 18.643 15.738C18.99 16.259 19.455 16.691 20 17H4C4.545 16.691 5.01 16.259 5.357 15.738C5.705 15.217 5.925 14.622 6 14V11C6.06 9.731 6.463 8.502 7.168 7.445C7.873 6.388 8.852 5.543 10 5Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 17V18C9 18.796 9.316 19.559 9.879 20.121C10.441 20.684 11.204 21 12 21C12.796 21 13.559 20.684 14.121 20.121C14.684 19.559 15 18.796 15 18V17"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'settings':
        return (
          <>
            <Path
              d="M10.325 4.317C10.751 2.561 13.249 2.561 13.675 4.317C13.739 4.581 13.864 4.826 14.041 5.032C14.217 5.238 14.44 5.4 14.691 5.504C14.941 5.608 15.213 5.651 15.484 5.63C15.754 5.609 16.016 5.524 16.248 5.383C17.791 4.443 19.558 6.209 18.618 7.753C18.477 7.985 18.392 8.246 18.372 8.517C18.351 8.787 18.394 9.059 18.498 9.309C18.601 9.56 18.763 9.783 18.969 9.959C19.175 10.136 19.419 10.261 19.683 10.325C21.439 10.751 21.439 13.249 19.683 13.675C19.419 13.739 19.174 13.864 18.968 14.041C18.762 14.217 18.6 14.44 18.496 14.691C18.392 14.941 18.349 15.213 18.37 15.484C18.391 15.754 18.476 16.016 18.617 16.248C19.557 17.791 17.791 19.558 16.247 18.618C16.015 18.477 15.754 18.392 15.483 18.372C15.213 18.351 14.941 18.394 14.691 18.498C14.44 18.601 14.217 18.763 14.041 18.969C13.864 19.175 13.739 19.419 13.675 19.683C13.249 21.439 10.751 21.439 10.325 19.683C10.261 19.419 10.136 19.174 9.959 18.968C9.783 18.762 9.56 18.6 9.309 18.496C9.059 18.392 8.787 18.349 8.516 18.37C8.246 18.391 7.984 18.476 7.752 18.617C6.209 19.557 4.442 17.791 5.382 16.247C5.523 16.015 5.608 15.754 5.628 15.483C5.649 15.213 5.606 14.941 5.502 14.691C5.399 14.44 5.237 14.217 5.031 14.041C4.825 13.864 4.581 13.739 4.317 13.675C2.561 13.249 2.561 10.751 4.317 10.325C4.581 10.261 4.826 10.136 5.032 9.959C5.238 9.783 5.4 9.56 5.504 9.309C5.608 9.059 5.651 8.787 5.63 8.516C5.609 8.246 5.524 7.984 5.383 7.752C4.443 6.209 6.209 4.442 7.753 5.382C8.753 5.99 10.049 5.452 10.325 4.317Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Circle cx="12" cy="12" r="3" stroke={color} strokeWidth={strokeWidth} />
          </>
        );

      case 'user':
        return (
          <>
            <Circle cx="12" cy="8" r="4" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M6 21V19C6 17.939 6.421 16.922 7.172 16.172C7.922 15.421 8.939 15 10 15H14C15.061 15 16.078 15.421 16.828 16.172C17.579 16.922 18 17.939 18 19V21"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'privacy':
        return (
          <Path
            d="M12 3C14.336 5.067 17.384 6.143 20.5 6C20.954 7.543 21.092 9.161 20.908 10.759C20.724 12.357 20.22 13.901 19.427 15.301C18.634 16.7 17.568 17.925 16.292 18.904C15.016 19.884 13.557 20.596 12 21C10.443 20.596 8.983 19.884 7.708 18.904C6.432 17.925 5.365 16.7 4.573 15.301C3.78 13.901 3.276 12.357 3.092 10.759C2.908 9.161 3.046 7.543 3.5 6C6.615 6.143 9.664 5.067 12 3Z"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'sun':
        return (
          <>
            <Circle cx="12" cy="12" r="4" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M3 12H4M12 3V4M20 12H21M12 20V21M5.6 5.6L6.3 6.3M18.4 5.6L17.7 6.3M17.7 17.7L18.4 18.4M6.3 17.7L5.6 18.4"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'sunrise':
        return (
          <>
            {/* Left ray */}
            <Path
              d="M3 12H4"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Top ray */}
            <Path
              d="M12 3V4"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Right ray */}
            <Path
              d="M20 12H21"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Left diagonal ray */}
            <Path
              d="M5.6 5.6L6.3 6.3"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Right diagonal ray */}
            <Path
              d="M18.4 5.6L17.7 6.3"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Half sun (semicircle) */}
            <Path
              d="M8 12C8 10.939 8.421 9.922 9.172 9.172C9.922 8.421 10.939 8 12 8C13.061 8 14.078 8.421 14.828 9.172C15.579 9.922 16 10.939 16 12"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* First horizon line */}
            <Path
              d="M3 16H21"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            {/* Second horizon line */}
            <Path
              d="M3 20H21"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'moon':
        return (
          <Path
            d="M12 3C12.132 3 12.263 3 12.393 3C11.108 4.194 10.283 5.8 10.059 7.539C9.836 9.279 10.229 11.041 11.171 12.521C12.112 14 13.542 15.103 15.213 15.637C16.883 16.172 18.688 16.104 20.313 15.446C19.688 16.95 18.666 18.257 17.356 19.226C16.047 20.195 14.498 20.791 12.877 20.949C11.255 21.108 9.621 20.823 8.149 20.125C6.677 19.428 5.421 18.344 4.517 16.989C3.612 15.634 3.092 14.058 3.013 12.431C2.933 10.804 3.297 9.185 4.065 7.749C4.834 6.312 5.977 5.11 7.375 4.273C8.772 3.435 10.371 2.992 12 2.992V3Z"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'info':
        return (
          <>
            <Circle cx="12" cy="12" r="9" stroke={color} strokeWidth={strokeWidth} />
            <Circle cx="12" cy="8" r="1" fill={color} />
            <Path
              d="M11 11H12V17H13"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'home':
        return (
          <>
            <Path
              d="M5 12H3L12 3L21 12H19"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M5 12V19C5 19.53 5.211 20.039 5.586 20.414C5.961 20.789 6.47 21 7 21H17C17.53 21 18.039 20.789 18.414 20.414C18.789 20.039 19 19.53 19 19V12"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 21V15C9 14.47 9.211 13.961 9.586 13.586C9.961 13.211 10.47 13 11 13H13C13.53 13 14.039 13.211 14.414 13.586C14.789 13.961 15 14.47 15 15V21"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'search':
        return (
          <>
            <Circle cx="10" cy="10" r="7" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M21 21L15 15"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'cart':
        return (
          <>
            <Circle cx="9" cy="20" r="2" stroke={color} strokeWidth={strokeWidth} />
            <Circle cx="18" cy="20" r="2" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M18 18H9V4H7"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M9 6L21 7L20 14H9"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'sync':
        return (
          <>
            <Path
              d="M19.933 13.041C19.744 14.481 19.167 15.842 18.263 16.979C17.359 18.116 16.163 18.985 14.803 19.494C13.443 20.003 11.97 20.131 10.542 19.867C9.114 19.602 7.785 18.953 6.697 17.99C5.61 17.028 4.805 15.787 4.37 14.402C3.934 13.016 3.883 11.538 4.223 10.127C4.564 8.715 5.282 7.422 6.301 6.387C7.32 5.353 8.601 4.615 10.008 4.253C13.907 3.253 17.943 5.26 19.433 9"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M20 4V9H15"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'umbrella':
        return (
          <>
            <Path
              d="M4 12C4 9.878 4.843 7.843 6.343 6.343C7.843 4.843 9.878 4 12 4C14.122 4 16.157 4.843 17.657 6.343C19.157 7.843 20 9.878 20 12H4Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M12 12V18C12 18.53 12.211 19.039 12.586 19.414C12.961 19.789 13.47 20 14 20C14.53 20 15.039 19.789 15.414 19.414C15.789 19.039 16 18.53 16 18"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'cloud':
        return (
          <Path
            d="M6.657 18C4.085 18 2 15.993 2 13.517C2 11.042 4.085 9.035 6.657 9.035C7.05 7.273 8.451 5.835 10.332 5.262C12.212 4.69 14.288 5.069 15.776 6.262C17.264 7.452 17.938 9.269 17.546 11.031H18.536C20.449 11.031 22 12.591 22 14.517C22 16.444 20.449 18.004 18.535 18.004H6.657"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'upload':
        return (
          <>
            <Path
              d="M4 17V19C4 19.53 4.211 20.039 4.586 20.414C4.961 20.789 5.47 21 6 21H18C18.53 21 19.039 20.789 19.414 20.414C19.789 20.039 20 19.53 20 19V17"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M7 9L12 4L17 9"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M12 4V16"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'check':
        return (
          <Path
            d="M5 12L10 17L20 7"
            stroke={color}
            strokeWidth={strokeWidth}
            strokeLinecap={strokeLinecap}
            strokeLinejoin={strokeLinejoin}
          />
        );

      case 'lock':
        return (
          <>
            <Rect
              x="5"
              y="11"
              width="14"
              height="10"
              rx="2"
              stroke={color}
              strokeWidth={strokeWidth}
            />
            <Circle cx="12" cy="16" r="1" fill={color} />
            <Path
              d="M8 11V7C8 5.939 8.421 4.922 9.172 4.172C9.922 3.421 10.939 3 12 3C13.061 3 14.078 3.421 14.828 4.172C15.579 4.922 16 5.939 16 7V11"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'lock-open':
        return (
          <>
            <Rect
              x="5"
              y="11"
              width="14"
              height="10"
              rx="2"
              stroke={color}
              strokeWidth={strokeWidth}
            />
            <Circle cx="12" cy="16" r="1" fill={color} />
            <Path
              d="M8 11V6C8 4.939 8.421 3.922 9.172 3.172C9.922 2.421 10.939 2 12 2C13.061 2 14.078 2.421 14.828 3.172C15.579 3.922 16 4.939 16 6"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'compass':
        return (
          <>
            <Circle cx="12" cy="12" r="9" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M8 16L10 10L16 8L14 14L8 16Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'file':
        return (
          <>
            <Path
              d="M14 3V7C14 7.265 14.105 7.52 14.293 7.707C14.48 7.895 14.735 8 15 8H19"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M17 21H7C6.47 21 5.961 20.789 5.586 20.414C5.211 20.039 5 19.53 5 19V5C5 4.47 5.211 3.961 5.586 3.586C5.961 3.211 6.47 3 7 3H14L19 8V19C19 19.53 18.789 20.039 18.414 20.414C18.039 20.789 17.53 21 17 21Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'edit':
        return (
          <>
            <Path
              d="M7 7H6C5.47 7 4.961 7.211 4.586 7.586C4.211 7.961 4 8.47 4 9V18C4 18.53 4.211 19.039 4.586 19.414C4.961 19.789 5.47 20 6 20H15C15.53 20 16.039 19.789 16.414 19.414C16.789 19.039 17 18.53 17 18V17"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M20.385 6.585C20.779 6.191 21 5.657 21 5.1C21 4.543 20.779 4.009 20.385 3.615C19.991 3.221 19.457 3 18.9 3C18.343 3 17.809 3.221 17.415 3.615L9 12V15H12L20.385 6.585Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M16 5L19 8"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'alert':
        return (
          <>
            <Circle cx="12" cy="12" r="9" stroke={color} strokeWidth={strokeWidth} />
            <Path
              d="M12 8V12"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
            <Circle cx="12" cy="16" r="1" fill={color} />
          </>
        );

      case 'help':
        return (
          <>
            <Circle cx="12" cy="12" r="9" stroke={color} strokeWidth={strokeWidth} />
            <Circle cx="12" cy="17" r="1" fill={color} />
            <Path
              d="M12 14C12.45 14.001 12.887 13.851 13.241 13.573C13.594 13.296 13.844 12.907 13.95 12.47C14.056 12.033 14.011 11.573 13.823 11.164C13.635 10.755 13.315 10.422 12.914 10.218C12.516 10.014 12.061 9.951 11.623 10.039C11.185 10.126 10.789 10.36 10.5 10.701"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
              strokeLinejoin={strokeLinejoin}
            />
          </>
        );

      case 'closet':
        return (
          <>
            <Path
              d="M4 2H12V20H4C3.44772 20 3 19.5523 3 19V3C3 2.44772 3.44772 2 4 2Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinejoin={strokeLinejoin}
            />
            <Path
              d="M20 2C20.5523 2 21 2.44772 21 3V19C21 19.5523 20.5523 20 20 20H12V2H20Z"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinejoin={strokeLinejoin}
            />
            <Line
              x1="9"
              y1="11"
              x2="9"
              y2="12"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
            />
            <Line
              x1="7"
              y1="21"
              x2="7"
              y2="22"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
            />
            <Line
              x1="15"
              y1="11"
              x2="15"
              y2="12"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
            />
            <Line
              x1="17"
              y1="21"
              x2="17"
              y2="22"
              stroke={color}
              strokeWidth={strokeWidth}
              strokeLinecap={strokeLinecap}
            />
          </>
        );

      case 'google':
        return (
          <Path
            d="M21.8055 10.0415H21V10H12V14H17.6515C16.827 16.3285 14.6115 18 12 18C8.6865 18 6 15.3135 6 12C6 8.6865 8.6865 6 12 6C13.5295 6 14.921 6.577 15.9805 7.5195L18.809 4.691C17.023 3.0265 14.634 2 12 2C6.4775 2 2 6.4775 2 12C2 17.5225 6.4775 22 12 22C17.5225 22 22 17.5225 22 12C22 11.3295 21.931 10.675 21.8055 10.0415Z"
            fill={color}
          />
        );

      default:
        return null;
    }
  };

  return (
    <Svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      style={style}
      pointerEvents="none"
    >
      {renderIcon()}
    </Svg>
  );
}

export default Icon;
