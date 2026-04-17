import { Dimensions } from 'react-native';

const { width: SCREEN_WIDTH } = Dimensions.get('window');

export { SCREEN_WIDTH };
export const CONTAINER_PADDING = 15;
// Cap the visible card width so the layout still looks like a phone on tablets
// and on the web. The FlatList page is still SCREEN_WIDTH wide (so paging
// snaps correctly), but cardInner is centered inside the page and capped here.
export const MAX_CARD_WIDTH = 420;
