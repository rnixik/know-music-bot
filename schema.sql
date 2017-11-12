CREATE TABLE `songs` (
	`id` INT(11) NOT NULL AUTO_INCREMENT,
	`artist` VARCHAR(255) NOT NULL,
	`title` VARCHAR(255) NOT NULL,
	`lyrics` TEXT NOT NULL,
	`lang` VARCHAR(255) NULL DEFAULT 'en',
	`genre` VARCHAR(255) NULL DEFAULT 'alternative_rock',
	PRIMARY KEY (`id`),
	INDEX `lang` (`lang`),
	INDEX `genre` (`genre`)
)
COLLATE='utf8_general_ci'
ENGINE=InnoDB;
