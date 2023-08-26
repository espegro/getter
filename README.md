# getter
Webservice to store and serve images

A simple webservice written in Go.

It can take an image as a POST. Store it in a given directory. It will validate that it is a JPEG and below max size.

It can also serve images from the same directory.

It has a crop function to only serve a x1,y1 to x2,y2 box of the image.

It has a "w" param to scale the resulting box to max w pixels.

It has a id=true param to print the full header on the image.

It has a nolabel=true param to not print anything

It has a color=FF00FF param to print the label in given color.

